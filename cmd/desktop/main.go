package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/buildinfo"
	"github.com/lunitide/lunitide/internal/credentialsubmission"
	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/engineclient"
	"github.com/lunitide/lunitide/internal/hostbridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/internal/secretlease"
	"github.com/lunitide/lunitide/internal/webviewhost"
	"github.com/oklog/ulid/v2"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	enginePath := flag.String("engine", filepath.Join(filepath.Dir(executable), "lunitide-engine.exe"), "engine executable")
	pipe := flag.String("pipe", "", "development-only named pipe override")
	flag.Parse()
	pipeID := make([]byte, 16)
	if _, err := rand.Read(pipeID); err != nil {
		return err
	}
	if *pipe == "" {
		*pipe = `\\.\pipe\lunitide-engine-` + hex.EncodeToString(pipeID)
	}
	brokerID := make([]byte, 16)
	if _, err := rand.Read(brokerID); err != nil {
		return err
	}
	brokerPipe := `\\.\pipe\lunitide-secret-` + hex.EncodeToString(brokerID)
	brokerListener, err := ipc.ListenCurrentUser(brokerPipe)
	if err != nil {
		return err
	}
	defer brokerListener.Close()
	bootstrapReader, bootstrapWriter, nonce, err := ipc.NewSessionBootstrap()
	if err != nil {
		return err
	}
	defer zeroBytes(nonce)
	defer bootstrapReader.Close()
	defer bootstrapWriter.Close()
	command := exec.Command(*enginePath, "--pipe", *pipe, "--host-pid", fmt.Sprint(os.Getpid()))
	command.Stdin = bootstrapReader
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		return err
	}
	defer stopProcess(command)
	if err := bootstrapReader.Close(); err != nil {
		return err
	}
	if err := ipc.WriteLaunchBootstrap(bootstrapWriter, nonce, brokerPipe); err != nil {
		return fmt.Errorf("write Engine bootstrap secret: %w", err)
	}
	if err := bootstrapWriter.Close(); err != nil {
		return err
	}
	dataRoot, err := datadir.PrepareProduction()
	if err != nil {
		return err
	}
	defer dataRoot.Close()
	webViewDataRoot, err := dataRoot.PrepareSubdirectory("WebView2")
	if err != nil {
		return err
	}
	defer webViewDataRoot.Close()
	secretService, err := secret.NewDPAPIService(dataRoot)
	if err != nil {
		return err
	}
	brokerKey := secretlease.DeriveKey(nonce)
	defer secret.Zero(brokerKey)
	broker, err := secretlease.NewServer(brokerListener, secretService, command.Process.Pid, brokerKey)
	if err != nil {
		return err
	}
	brokerCtx, stopBroker := context.WithCancel(context.Background())
	defer broker.Close()
	defer stopBroker()
	brokerErr := make(chan error, 1)
	go func() {
		if err := broker.Serve(brokerCtx); err != nil {
			brokerErr <- err
		}
	}()
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	client, err := engineclient.Connect(startupCtx, *pipe, command.Process.Pid, hex.EncodeToString(nonce))
	cancelStartup()
	zeroBytes(nonce)
	if err != nil {
		return err
	}
	defer client.Close()
	resolver := credentialsubmission.RPCResolver{Engine: client}
	coordinator, err := credentialsubmission.New(dataRoot, secretService, resolver, resolver)
	if err != nil {
		return err
	}
	defer coordinator.Close()
	credentialHandler := &credentialsubmission.HostHandler{Coordinator: coordinator, Engine: client, Secrets: secretService}
	migrationCtx, cancelMigration := context.WithTimeout(context.Background(), 20*time.Second)
	migrationErr := credentialHandler.RunElectronCredentialAdoption(migrationCtx)
	cancelMigration()
	if migrationErr != nil {
		// The underlying discovery/platform error may contain a legacy path.
		// Pending items remain retryable on the next startup.
		fmt.Fprintln(os.Stderr, "Electron credential migration was deferred and will be retried on next startup")
	}
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	credentialHandler.StartCleanupWorker(cleanupCtx)
	id := ulid.Make().String()
	request := bridge.Request{Version: bridge.Version, Kind: "request", ID: id, TraceID: ulid.Make().String(), Method: "system.health", SentAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000}
	requestCtx, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRequest()
	response, err := client.Call(requestCtx, request)
	if err != nil {
		return err
	}
	if !response.OK {
		return errors.New("Engine health check failed")
	}
	rendererDir, err := webviewhost.DefaultRendererFolder()
	if err != nil {
		return err
	}
	gateway, err := hostbridge.New(webviewhost.TrustedOrigin, client, map[bridge.Method]hostbridge.Handler{
		bridge.MethodProviderCredentialSubmit: credentialHandler,
		bridge.MethodProviderCreate:           credentialHandler,
		bridge.MethodProviderUpdate:           credentialHandler,
		bridge.MethodProviderDelete:           credentialHandler,
	})
	if err != nil {
		return err
	}
	host, err := webviewhost.New(gateway, rendererDir, webViewDataRoot.Path())
	if err != nil {
		return err
	}
	fmt.Printf("Lunitide %s: Engine health check passed; starting WebView2 host\n", buildinfo.Version)
	hostCtx, stopHost := context.WithCancel(context.Background())
	defer stopHost()
	hostErr := make(chan error, 1)
	go func() { hostErr <- host.Run(hostCtx) }()
	select {
	case err := <-brokerErr:
		stopHost()
		<-hostErr
		return fmt.Errorf("secret broker stopped unexpectedly: %w", err)
	case err := <-hostErr:
		return err
	}
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func stopProcess(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	done := make(chan struct{})
	go func() { _ = command.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}
