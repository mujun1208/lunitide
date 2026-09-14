package modelquality

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

type LiveCallResult struct {
	Text       string
	Latency    time.Duration
	HTTPClass  string
	HTTPStatus int
	Usage      *TokenUsage
	ErrorCode  string
}

type LiveSecretsFunc func(ctx context.Context, row InstalledProvider, fn func([]byte) error) error
type LiveCallFunc func(ctx context.Context, tgt AuthorizedTarget, secret []byte, prompt string, maxTokens int) (LiveCallResult, error)
type LiveProbeFunc func(ctx context.Context, tgt AuthorizedTarget, secret []byte) (ProbeRecord, error)

func RunLiveQ(ctx context.Context, rows []InstalledProvider, secrets LiveSecretsFunc, probe LiveProbeFunc, call LiveCallFunc, onlyModel string) (LiveEvidence, error) {
	suite, err := LoadEvalSuite()
	if err != nil {
		return LiveEvidence{}, err
	}
	sel := SelectAuthorizedTargets(rows)
	ev := LiveEvidence{
		SchemaVersion:   1,
		CheckedAt:       time.Now().UTC().Format(time.RFC3339),
		LiveQualified:   false,
		ProfileMismatch: sel.ProfileMismatch,
		Rejected:        sel.Rejected,
		Notes: []string{
			"layers.Q is never live_qualified",
			"profile testdata lists deepseek-v4-flash; installed DeepSeek default is deepseek-v4-pro and was not relabeled",
			"fixtureOnly and missing-glyph do not increment LiveSuccess",
			"thinking disabled so closeoutOutputTokens apply to the deliverable",
		},
	}

	byID := map[string]InstalledProvider{}
	for _, row := range rows {
		byID[row.ID] = row
	}

	for _, tgt := range sel.Targets {
		if onlyModel != "" && tgt.ModelID != onlyModel {
			continue
		}
		row, ok := byID[tgt.ProviderID]
		if !ok {
			ev.Probes = append(ev.Probes, ProbeRecord{
				ProviderName: tgt.ProviderName, OriginHost: tgt.OriginHost, ModelID: tgt.ModelID, Role: tgt.Role,
				Status: "failed", ErrorCode: "PROVIDER_MISSING",
			})
			continue
		}
		err := secrets(ctx, row, func(secret []byte) error {
			pr, perr := probe(ctx, tgt, secret)
			pr.ProviderName = tgt.ProviderName
			pr.OriginHost = tgt.OriginHost
			pr.ModelID = tgt.ModelID
			pr.Role = tgt.Role
			if pr.ErrorCode == "" && perr != nil {
				pr.ErrorCode = "PROBE_FAILED"
				pr.Status = "failed"
			}
			if pr.Status == "" {
				if perr == nil {
					pr.Status = "ok"
				} else {
					pr.Status = "failed"
				}
			}
			pr.ErrorCode = sanitizeErrorCode(pr.ErrorCode)
			ev.Probes = append(ev.Probes, pr)
			if perr != nil || pr.Status != "ok" || tgt.Role != "suite" {
				return nil
			}
			run := runSuiteOnce(ctx, suite, tgt, secret, call)
			ev.Runs = append(ev.Runs, run)
			return nil
		})
		if err != nil {
			ev.Probes = append(ev.Probes, ProbeRecord{
				ProviderName: tgt.ProviderName, OriginHost: tgt.OriginHost, ModelID: tgt.ModelID, Role: tgt.Role,
				Status: "failed", ErrorCode: classifySecretError(err),
			})
		}
	}
	ev.LayersQ = LayersQToken(ev.Probes, ev.Runs)
	return ev, nil
}

func runSuiteOnce(ctx context.Context, suite *Suite, tgt AuthorizedTarget, secret []byte, call LiveCallFunc) TargetRun {
	run := TargetRun{ProviderName: tgt.ProviderName, OriginHost: tgt.OriginHost, ModelID: tgt.ModelID}
	for _, id := range suite.CaseIDs() {
		c := suite.MustCase(id)
		fmt.Printf("suite %s %s\n", tgt.ModelID, id)
		_ = os.Stdout.Sync()
		scenario := LiveScenario(c)
		rec := CaseRecord{CaseID: id, ScenarioID: scenario, FixtureOnly: c.FixtureOnly}
		cap, raised := LiveOutputTokenCap(c)
		rec.OutputTokensCap = cap
		rec.CapRaised = raised
		if c.FixtureOnly {
			rec.Status = "skipped_fixture_only"
			rec.EvidenceKind = "skipped_fixture_only"
			rec.ErrorCode = "fixture_only"
			suite.Record(&run.Accounting, Outcome{CaseID: id, ScenarioID: scenario, FixtureOnly: true, OraclePass: false})
			run.Skipped++
			run.Cases = append(run.Cases, rec)
			continue
		}
		if hostRuntimeOnly(id) {
			rec.Status = "needs_host_runtime"
			rec.EvidenceKind = "needs_host_runtime"
			rec.ErrorCode = "needs_host_runtime"
			suite.Record(&run.Accounting, Outcome{CaseID: id, ScenarioID: scenario, OraclePass: false})
			run.LiveFail++
			run.Cases = append(run.Cases, rec)
			continue
		}
		if ctx.Err() != nil {
			rec.Status = "oracle_fail"
			rec.ErrorCode = "CONTEXT_CANCELED"
			run.LiveFail++
			suite.Record(&run.Accounting, Outcome{CaseID: id, ScenarioID: scenario, OraclePass: false})
			run.Cases = append(run.Cases, rec)
			continue
		}
		callRes, err := call(ctx, tgt, secret, LiveCasePrompt(c), cap)
		rec.LatencyMS = callRes.Latency.Milliseconds()
		rec.HTTPClass = callRes.HTTPClass
		rec.TokenUsage = callRes.Usage
		if err != nil || callRes.ErrorCode != "" && callRes.Text == "" {
			rec.Status = "oracle_fail"
			rec.ErrorCode = callRes.ErrorCode
			if rec.ErrorCode == "" {
				rec.ErrorCode = "REQUEST_FAILED"
			}
			rec.ErrorCode = sanitizeErrorCode(rec.ErrorCode)
			run.LiveFail++
			suite.Record(&run.Accounting, Outcome{CaseID: id, ScenarioID: scenario, OraclePass: false})
			run.Cases = append(run.Cases, rec)
			continue
		}
		_, verdict, kind := ObserveLiveCase(suite, c, callRes.Text)
		rec.OraclePass = verdict.Pass
		rec.LiveSuccessIncrement = verdict.LiveSuccessIncrement
		rec.EvidenceKind = kind
		rec.Status = kind
		rec.Reasons = verdict.Reasons
		if callRes.ErrorCode != "" {
			rec.ErrorCode = sanitizeErrorCode(callRes.ErrorCode)
		} else if !verdict.Pass && len(verdict.Reasons) > 0 {
			rec.ErrorCode = sanitizeErrorCode(verdict.Reasons[0])
		}
		suite.Record(&run.Accounting, Outcome{
			CaseID: id, ScenarioID: scenario, FixtureOnly: c.FixtureOnly,
			OraclePass: verdict.Pass, Verdict: &verdict, EvidenceKind: "live",
		})
		if verdict.LiveSuccessIncrement {
			run.LivePass++
		} else {
			run.LiveFail++
		}
		run.Cases = append(run.Cases, rec)
	}
	run.Accounting.LiveQualified = false
	run.Accounting.EvalComplete = false
	run.Accounting.HoldOutComplete = false
	return run
}

func hostRuntimeOnly(id string) bool {
	switch id {
	case "R01", "R02", "R03", "R04", "L01", "L02", "L03", "L04":
		return true
	default:
		return false
	}
}

func classifySecretError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "not exist") || strings.Contains(s, "not found"):
		return "CREDENTIAL_MISSING"
	case strings.Contains(s, "unavailable") || strings.Contains(s, "binding mismatch"):
		return "CREDENTIAL_UNAVAILABLE"
	case strings.Contains(s, "invalid secret") || strings.Contains(s, "invalid credential"):
		return "CREDENTIAL_INVALID"
	default:
		return "SECRET_UNAVAILABLE"
	}
}
