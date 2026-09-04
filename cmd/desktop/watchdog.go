package main

import (
	"context"
	"encoding/hex"
	"errors"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/lunitide/lunitide/internal/engineclient"
	"github.com/lunitide/lunitide/internal/hostbridge"
	"github.com/lunitide/lunitide/internal/ipc"
)

const (
	flagTakeover        = "--takeover"
	gatewayTakeoverWait = 15 // seconds; backup if the dying host forgets to drop the mutex
)

var errNoDesktopSelf = errors.New("no desktop executable path")

// engineWatchShouldRelaunch is D11 after G1: killing the tray/window must
// leave a live engine alone. Only a dead engine (RPC poison or PID gone)
// makes the desktop/tray spawn a replacement.
func engineWatchShouldRelaunch(shuttingDown, rpcBroken bool, pid int, pidAlive bool) bool {
	if shuttingDown {
		return false
	}
	if rpcBroken {
		return true
	}
	if pid > 0 && !pidAlive {
		return true
	}
	return false
}

// engineWatchPreferReconnect is T0: a poisoned client with a still-living
// engine PID should get a new RPC client + ReplaceCaller before the desktop
// drops the mutex and --takeover-restarts itself.
func engineWatchPreferReconnect(rpcBroken, pidAlive bool) bool {
	return rpcBroken && pidAlive
}

func watchEngineHealth(hostCtx context.Context, shuttingDown *atomic.Bool, engineClient *atomic.Pointer[engineclient.Client], enginePID *atomic.Int32, pidPath, pipe, noncePath string, gateway *hostbridge.Gateway, onDeath func(rpcBroken bool, pid int)) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-hostCtx.Done():
			return
		case <-ticker.C:
			pid := int(enginePID.Load())
			if pid < 1 && pidPath != "" {
				if saved, err := ipc.LoadEnginePID(pidPath); err == nil {
					pid = saved
					enginePID.Store(int32(pid))
				}
			}
			rpcBroken := false
			if c := engineClient.Load(); c != nil {
				rpcBroken = c.Broken() != nil
			}
			alive := processAlive(pid)
			if !engineWatchShouldRelaunch(shuttingDown.Load(), rpcBroken, pid, alive) {
				continue
			}
			if engineWatchPreferReconnect(rpcBroken, alive) && tryReplaceEngineCaller(hostCtx, pipe, noncePath, gateway, engineClient, enginePID) {
				hostLog("poisoned RPC replaced in-process; leaving the engine PID")
				continue
			}
			if onDeath != nil {
				onDeath(rpcBroken, pid)
			}
			return
		}
	}
}

func tryReplaceEngineCaller(ctx context.Context, pipe, noncePath string, gateway *hostbridge.Gateway, engineClient *atomic.Pointer[engineclient.Client], enginePID *atomic.Int32) bool {
	nonce, err := ipc.LoadGatewayNonce(noncePath)
	if err != nil || len(nonce) != 32 {
		return false
	}
	nonceHex := hex.EncodeToString(nonce)
	zeroBytes(nonce)
	dial, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	next, err := engineclient.Connect(dial, pipe, 0, nonceHex)
	if err != nil || next == nil {
		return false
	}
	if gateway != nil {
		gateway.ReplaceCaller(next)
	}
	old := engineClient.Swap(next)
	if old != nil && old != next {
		_ = old.Close()
	}
	if pid, pidErr := next.ServerPID(); pidErr == nil && pid > 0 {
		enginePID.Store(int32(pid))
	}
	return true
}

// desktopTakeoverArgs is D11: the replacement desktop must wait for this
// process to drop the singleton mutex. Starting a sibling without --takeover
// hits "already running", activates the dying window, and exits — then this
// process stopHost() and nothing is left to respawn the engine.
func desktopTakeoverArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	for _, arg := range args {
		if arg == flagTakeover {
			continue
		}
		out = append(out, arg)
	}
	return append(out, flagTakeover)
}

// releaseGatewayThenRelaunch is D11: drop Local\lunitide-gateway before the
// sibling starts. The 0.4.43 failure was a dying WebView2 still holding the
// mutex for longer than the child would wait, so --takeover timed out and
// nothing respawned the engine.
func releaseGatewayThenRelaunch(self string, args []string, release func(), start func(*exec.Cmd) error) error {
	if release != nil {
		release()
	}
	if self == "" {
		return errNoDesktopSelf
	}
	if start == nil {
		start = func(cmd *exec.Cmd) error { return cmd.Start() }
	}
	return start(exec.Command(self, desktopTakeoverArgs(args)...))
}
