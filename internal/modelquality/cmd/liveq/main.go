//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/modelquality"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/secret"
)

func main() {
	dataRoot := flag.String("data-root", defaultDataRoot(), "installed Lunitide data directory")
	out := flag.String("out", filepath.Join("docs", "audits", "model-office-upgrade", "q-live-evidence.json"), "evidence JSON path")
	onlyModel := flag.String("only-model", "", "optional model id filter (rerun one target)")
	flag.Parse()
	if err := run(*dataRoot, *out, *onlyModel); err != nil {
		fmt.Fprintln(os.Stderr, modelquality.SanitizeLog(err.Error()))
		os.Exit(1)
	}
}

func defaultDataRoot() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Lunitide")
}

func run(dataRoot, outPath, onlyModel string) error {
	dataRoot = filepath.Clean(dataRoot)
	if !filepath.IsAbs(dataRoot) {
		return fmt.Errorf("data-root must be absolute")
	}
	if filepath.Clean(dataRoot) != filepath.Clean(defaultDataRoot()) {
		if err := os.Setenv("LUNITIDE_DATA_ROOT", dataRoot); err != nil {
			return err
		}
	}

	ctx := context.Background()
	logf("reading providers from installed db (mode=ro)\n")
	rows, err := modelquality.ReadInstalledProviders(ctx, modelquality.InstalledDBPath(dataRoot))
	if err != nil {
		return fmt.Errorf("read providers: %w", err)
	}
	logf("installed_rows=%d\n", len(rows))

	logf("preparing production data root (DPAPI only; no OpenSecure)\n")
	root, err := datadir.PrepareProduction()
	if err != nil {
		return fmt.Errorf("prepare data root: %w", err)
	}
	defer root.Close()
	vault, err := secret.NewDPAPIService(root)
	if err != nil {
		return err
	}

	secrets := func(ctx context.Context, row modelquality.InstalledProvider, fn func([]byte) error) error {
		origin, oerr := provider.NormalizeOrigin(row.BaseURL)
		if oerr != nil {
			return oerr
		}
		ref := secret.Ref{CredentialRef: row.CredentialRef, ProviderID: row.ID, Origin: origin, Protocol: row.Protocol}
		return vault.WithSecret(ctx, ref, fn)
	}

	probe := func(ctx context.Context, tgt modelquality.AuthorizedTarget, sec []byte) (modelquality.ProbeRecord, error) {
		logf("probing %s %s %s\n", tgt.ProviderName, tgt.OriginHost, tgt.ModelID)
		rec := modelquality.ProbeRecord{ProviderName: tgt.ProviderName, OriginHost: tgt.OriginHost, ModelID: tgt.ModelID, Role: tgt.Role}
		adapter, aerr := openAdapter(ctx, tgt.BaseURL)
		if aerr != nil {
			rec.Status = "failed"
			rec.ErrorCode = probeCode(aerr)
			return rec, aerr
		}
		start := time.Now()
		perr := adapter.TestConnection(ctx, sec, llmadapter.Request{
			Model: tgt.ModelID, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "ping"}},
			MaxTokens: 1, MaxAttempts: 1, DisableReasoning: true,
		})
		rec.LatencyMS = time.Since(start).Milliseconds()
		if perr != nil {
			rec.Status = "failed"
			rec.HTTPClass = httpClassOf(perr)
			rec.ErrorCode = probeCode(perr)
			logf("probe-result %s %s status=failed err=%s latencyMs=%d\n", tgt.ProviderName, tgt.ModelID, rec.ErrorCode, rec.LatencyMS)
			return rec, perr
		}
		rec.Status = "ok"
		rec.HTTPClass = "2xx"
		logf("probe-result %s %s status=ok latencyMs=%d\n", tgt.ProviderName, tgt.ModelID, rec.LatencyMS)
		return rec, nil
	}

	call := func(ctx context.Context, tgt modelquality.AuthorizedTarget, sec []byte, prompt string, maxTokens int) (modelquality.LiveCallResult, error) {
		logf("case-call %s %s cap=%d\n", tgt.ProviderName, tgt.ModelID, maxTokens)
		out := modelquality.LiveCallResult{}
		adapter, aerr := openAdapter(ctx, tgt.BaseURL)
		if aerr != nil {
			out.ErrorCode = probeCode(aerr)
			return out, aerr
		}
		start := time.Now()
		req := llmadapter.Request{
			Model: tgt.ModelID,
			Messages: []llmadapter.Message{
				{Role: llmadapter.RoleSystem, Content: "You are scoring a fixed eval case. Follow the user instructions exactly. Never echo credentials."},
				{Role: llmadapter.RoleUser, Content: prompt},
			},
			MaxTokens: maxTokens, MaxAttempts: 1, DisableReasoning: true,
		}
		resp, cerr := adapter.Complete(ctx, sec, req)
		if httpStatusOf(cerr) == 400 {
			req.DisableReasoning = false
			resp, cerr = adapter.Complete(ctx, sec, req)
		}
		out.Latency = time.Since(start)
		if cerr != nil {
			out.ErrorCode = probeCode(cerr)
			out.HTTPClass = httpClassOf(cerr)
			out.HTTPStatus = httpStatusOf(cerr)
			return out, cerr
		}
		out.Text = resp.Message.Content
		out.HTTPClass = "2xx"
		out.HTTPStatus = 200
		if resp.Usage.TotalTokens > 0 || resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0 {
			out.Usage = &modelquality.TokenUsage{
				InputTokens:  resp.Usage.InputTokens,
				OutputTokens: resp.Usage.OutputTokens,
				TotalTokens:  resp.Usage.TotalTokens,
			}
		}
		return out, nil
	}

	ev, err := modelquality.RunLiveQ(ctx, rows, secrets, probe, call, onlyModel)
	if err != nil {
		return err
	}
	if onlyModel != "" {
		ev = filterEvidence(ev, onlyModel)
		if merged, merr := mergeEvidenceFile(outPath, ev); merr == nil {
			ev = merged
		}
	}
	raw, err := modelquality.MarshalLiveEvidence(ev)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	baseline := filepath.Join(filepath.Dir(outPath), "baseline.json")
	if err := modelquality.WriteBaselineQ(baseline, ev.LayersQ); err != nil {
		return fmt.Errorf("baseline Q: %w", err)
	}
	printSummary(ev, outPath)
	return nil
}

func openAdapter(ctx context.Context, baseURL string) (*llmadapter.OpenAI, error) {
	return llmadapter.OpenAIEndpoint(ctx, baseURL, networkpolicy.Options{
		ConnectTimeout:        15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		OverallTimeout:        180 * time.Second,
		MaxResponseBytes:      8 << 20,
	}, llmadapter.Options{MaxAttempts: 1})
}

func probeCode(err error) string {
	if err == nil {
		return ""
	}
	var ge *llmadapter.Error
	if errors.As(err, &ge) {
		if ge.Code != "" {
			return ge.Code
		}
		if ge.HTTPStatus > 0 {
			return "HTTP_" + strconv.Itoa(ge.HTTPStatus)
		}
	}
	var ne *networkpolicy.Error
	if errors.As(err, &ne) && ne.Code != "" {
		return string(ne.Code)
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "not exist"):
		return "CREDENTIAL_MISSING"
	default:
		return "REQUEST_FAILED"
	}
}

func httpStatusOf(err error) int {
	var ge *llmadapter.Error
	if errors.As(err, &ge) {
		return ge.HTTPStatus
	}
	return 0
}

func httpClassOf(err error) string {
	return modelquality.HTTPClass(httpStatusOf(err))
}

func filterEvidence(ev modelquality.LiveEvidence, modelID string) modelquality.LiveEvidence {
	var probes []modelquality.ProbeRecord
	for _, p := range ev.Probes {
		if p.ModelID == modelID {
			probes = append(probes, p)
		}
	}
	var runs []modelquality.TargetRun
	for _, r := range ev.Runs {
		if r.ModelID == modelID {
			runs = append(runs, r)
		}
	}
	ev.Probes = probes
	ev.Runs = runs
	return ev
}

func mergeEvidenceFile(path string, patch modelquality.LiveEvidence) (modelquality.LiveEvidence, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return patch, err
	}
	var base modelquality.LiveEvidence
	if err := json.Unmarshal(raw, &base); err != nil {
		return patch, err
	}
	for _, p := range patch.Probes {
		replaced := false
		for i, old := range base.Probes {
			if old.ModelID == p.ModelID && old.OriginHost == p.OriginHost {
				base.Probes[i] = p
				replaced = true
				break
			}
		}
		if !replaced {
			base.Probes = append(base.Probes, p)
		}
	}
	for _, r := range patch.Runs {
		replaced := false
		for i, old := range base.Runs {
			if old.ModelID == r.ModelID && old.OriginHost == r.OriginHost {
				base.Runs[i] = r
				replaced = true
				break
			}
		}
		if !replaced {
			base.Runs = append(base.Runs, r)
		}
	}
	if len(patch.Notes) > 0 {
		base.Notes = append(base.Notes, "glm-5.3 rerun after HTTP_400 thinking-disable retry")
	}
	base.CheckedAt = patch.CheckedAt
	base.LayersQ = modelquality.LayersQToken(base.Probes, base.Runs)
	base.LiveQualified = false
	return base, nil
}

func logf(format string, args ...any) {
	fmt.Printf(format, args...)
	_ = os.Stdout.Sync()
}

func printSummary(ev modelquality.LiveEvidence, outPath string) {
	fmt.Printf("layers.Q=%s liveQualified=%t evidence=%s\n", ev.LayersQ, ev.LiveQualified, outPath)
	for _, p := range ev.Probes {
		fmt.Printf("probe %s %s %s status=%s latencyMs=%d http=%s err=%s\n",
			p.ProviderName, p.OriginHost, p.ModelID, p.Status, p.LatencyMS, p.HTTPClass, p.ErrorCode)
	}
	for _, r := range ev.Runs {
		fmt.Printf("run %s %s livePass=%d liveFail=%d skipped=%d liveSuccess=%d\n",
			r.ProviderName, r.ModelID, r.LivePass, r.LiveFail, r.Skipped, r.Accounting.LiveSuccess)
	}
}
