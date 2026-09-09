package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

type officeSnapshotTestPage struct {
	Task      struct{ ID, Goal string }
	Artifacts []struct {
		ID       string
		Versions []struct {
			ID                                 string
			ValidationOffset, TotalValidations int
			Validations                        []struct {
				ID, Message      string
				MessageTruncated bool
			}
		}
	}
	Steps []struct {
		ID, Summary      string
		SummaryTruncated bool
	}
	Sources []struct {
		ID, Transform      string
		TransformTruncated bool
	}
	Items                                                  []map[string]any
	SnapshotOffset, NextSnapshotOffset, TotalSnapshotItems int
	SnapshotDigest                                         string
}

func officeSnapshotArgsForTest(base map[string]any, offset int, digest string) map[string]any {
	base["snapshotOffset"] = offset
	if digest != "" {
		base["snapshotDigest"] = digest
	}
	return base
}

func officeSnapshotPageForTest(t *testing.T, r bridge.Response) officeSnapshotTestPage {
	t.Helper()
	if !r.OK {
		t.Fatalf("snapshot response: %+v", r.Error)
	}
	encoded, err := json.Marshal(r)
	if err != nil || len(encoded) > officeSnapshotFrameBytes {
		t.Fatalf("frame %d: %v", len(encoded), err)
	}
	var p officeSnapshotTestPage
	if err = decodeResponsePayload(r.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOfficeSnapshotBridgePagesEveryVersionReceiptAndSource(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "snapshot-task")
	task.Goal = strings.Repeat("完整目标", 600)
	var err error
	task, err = store.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	artifact := ulid.Make().String()
	const count = 1005
	var versions []domain.Version
	for i := 0; i < count; i++ {
		v, er := store.PublishOfficeVersion(ctx, domain.PublishRequest{Version: domain.Version{TaskID: task.ID, ArtifactID: artifact, Name: "报告.docx", Kind: "docx", ContentRef: "snapshot-fixture", MediaType: "application/octet-stream", SHA256: strings.Repeat("a", 64), Size: 1}, ExpectedHeadRevision: int64(i), IdempotencyKey: fmt.Sprintf("v-%d", i)})
		if er != nil {
			t.Fatal(er)
		}
		versions = append(versions, v)
	}
	checks := make([]domain.Check, 600)
	for i := range checks {
		checks[i] = domain.Check{ID: fmt.Sprintf("c-%d", i), Label: "检查", Status: "passed", Required: true, Detail: strings.Repeat("detail", 170)}
	}
	latest := versions[len(versions)-1]
	if _, err = store.AddOfficeValidation(ctx, domain.Validation{VersionID: latest.ID, SHA256: latest.SHA256, Validator: "fixture", Checks: checks}); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(map[string]string{"text": strings.Repeat("完整证据", 1500)})
	for i := 0; i < count; i++ {
		if err = store.AppendOfficeStepReceipt(ctx, domain.StepReceipt{TaskID: task.ID, RunID: task.ID, StepKey: "测试", IdempotencyKey: fmt.Sprintf("step-%d", i), InputDigest: strings.Repeat("b", 64), State: "succeeded", Result: result}); err != nil {
			t.Fatal(err)
		}
		if err = store.AddOfficeEvidenceEdge(ctx, domain.EvidenceEdge{TaskID: task.ID, SourceVersionID: versions[0].ID, TargetVersionID: latest.ID, SourceNode: fmt.Sprintf("node-%d", i), Metric: result}); err != nil {
			t.Fatal(err)
		}
	}
	seenVersions, seenChecks, seenSteps, seenSources := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	offset, digest, pages := 0, "", 0
	for {
		p := officeSnapshotPageForTest(t, officeCall(t, e, "office.task.get", "", officeSnapshotArgsForTest(map[string]any{"taskId": task.ID}, offset, digest)))
		if p.Task.Goal != task.Goal {
			t.Fatal("detail lost full task goal")
		}
		if pages > 0 && p.SnapshotDigest != digest {
			t.Fatal("unchanged snapshot drifted")
		}
		for _, a := range p.Artifacts {
			for _, v := range a.Versions {
				seenVersions[v.ID] = true
				for _, c := range v.Validations {
					if seenChecks[c.ID] {
						t.Fatal("validation repeated across page")
					}
					seenChecks[c.ID] = true
					if !c.MessageTruncated {
						t.Fatal("check summary unlabelled")
					}
				}
			}
		}
		for _, s := range p.Steps {
			if seenSteps[s.ID] {
				t.Fatal("step repeated across page")
			}
			seenSteps[s.ID] = true
			if !s.SummaryTruncated {
				t.Fatal("step summary unlabelled")
			}
		}
		for _, s := range p.Sources {
			if seenSources[s.ID] {
				t.Fatal("source repeated across page")
			}
			seenSources[s.ID] = true
			if !s.TransformTruncated {
				t.Fatal("source summary unlabelled")
			}
		}
		pages++
		if p.NextSnapshotOffset == -1 {
			break
		}
		if p.NextSnapshotOffset <= offset || pages > 100 {
			t.Fatal("snapshot cursor did not progress")
		}
		offset, digest = p.NextSnapshotOffset, p.SnapshotDigest
	}
	if len(seenVersions) != count || len(seenSteps) != count || len(seenSources) != count || len(seenChecks) != len(checks) || pages < 2 {
		t.Fatalf("lost records: versions=%d steps=%d sources=%d checks=%d pages=%d", len(seenVersions), len(seenSteps), len(seenSources), len(seenChecks), pages)
	}
	first := officeSnapshotPageForTest(t, officeCall(t, e, "office.task.get", "", map[string]any{"taskId": task.ID}))
	if _, err = store.AcceptOfficeVersion(ctx, task.ID, artifact, versions[0].ID, count); err != nil {
		t.Fatal(err)
	}
	changed := officeCall(t, e, "office.task.get", "", map[string]any{"taskId": task.ID, "snapshotOffset": first.NextSnapshotOffset, "snapshotDigest": first.SnapshotDigest})
	if changed.OK || changed.Error.Code != "OFFICE_SNAPSHOT_CHANGED" || !changed.Error.Retryable {
		t.Fatalf("mixed snapshot accepted: %+v", changed)
	}
	qa, err := store.ListOfficeValidations(ctx, latest.ID)
	if err != nil || qa[0].Checks[0].Detail != checks[0].Detail {
		t.Fatal("display abbreviation modified stored QA", err)
	}
}

func TestOfficeTaskListSnapshotBoundsGoalsAndKeepsAllTasks(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	base := officeCreatedTask(t, e, "tasks")
	const count = 230
	longGoal := strings.Repeat("要", 2600) + "needle-at-end"
	for i := 0; i < count; i++ {
		if _, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: base.SessionID, Title: fmt.Sprintf("任务-%03d", i), Goal: longGoal}, fmt.Sprintf("task-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	offset, digest, seen := 0, "", map[string]bool{}
	for {
		p := officeSnapshotPageForTest(t, officeCall(t, e, "office.task.list", "", officeSnapshotArgsForTest(map[string]any{"query": "needle-at-end"}, offset, digest)))
		if len(p.Items) > 200 {
			t.Fatal("list page exceeds 200 tasks")
		}
		for _, row := range p.Items {
			id := row["id"].(string)
			if seen[id] {
				t.Fatal("duplicate list item")
			}
			seen[id] = true
			if row["goalTruncated"] != true || len(row["goal"].(string)) > 260 {
				t.Fatalf("unbounded goal: %#v", row)
			}
		}
		if p.NextSnapshotOffset == -1 {
			break
		}
		if p.NextSnapshotOffset <= offset {
			t.Fatal("list cursor stalled")
		}
		offset, digest = p.NextSnapshotOffset, p.SnapshotDigest
	}
	if len(seen) != count {
		t.Fatalf("task list silently capped: %d", len(seen))
	}
}

func TestOfficeDeliverySnapshotBoundsLongMetricsAndBundles(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "deliveries")
	text := strings.Repeat("<>&", 1500)
	nodeDigest := strings.Repeat("c", 64)
	index, _ := json.Marshal(map[string]any{"nodes": []any{map[string]string{"id": "node", "kind": "text", "text": text, "digest": nodeDigest}}})
	v, err := store.PublishOfficeVersion(ctx, domain.PublishRequest{Version: domain.Version{TaskID: task.ID, Name: "报告.docx", Kind: "docx", ContentRef: "fixture", MediaType: "application/octet-stream", SHA256: strings.Repeat("a", 64), Size: 1, Index: index}, IdempotencyKey: "version"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err = store.CreateOfficeMetric(ctx, domain.Metric{TaskID: task.ID, Name: fmt.Sprintf("指标%d", i), SourceVersionID: v.ID, SourceSHA256: v.SHA256, SourceNodeID: "node", SourceNodeDigest: nodeDigest, RawValue: text, ValueType: "text", DisplayValue: text, Aggregation: "identity", RoundingPolicy: "none"}, fmt.Sprintf("metric-%d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err = store.CreateOfficeBundle(ctx, domain.Bundle{TaskID: task.ID, Title: fmt.Sprintf("交付%d", i), Files: []domain.BundleFile{{VersionID: v.ID, ArtifactID: v.ArtifactID, Name: v.Name, Kind: v.Kind, SHA256: v.SHA256, Size: v.Size}}}, fmt.Sprintf("bundle-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for _, method := range []string{"office.metric.list", "office.bundle.list"} {
		offset, digest, seen := 0, "", map[string]bool{}
		for {
			p := officeSnapshotPageForTest(t, officeCall(t, e, method, "", officeSnapshotArgsForTest(map[string]any{"taskId": task.ID}, offset, digest)))
			for _, item := range p.Items {
				id := item["id"].(string)
				if seen[id] {
					t.Fatal("duplicate delivery")
				}
				seen[id] = true
				if method == "office.metric.list" && (item["rawValueTruncated"] != true || item["displayValueTruncated"] != true) {
					t.Fatal("unlabelled metric abbreviation")
				}
			}
			if p.NextSnapshotOffset == -1 {
				break
			}
			if p.NextSnapshotOffset <= offset {
				t.Fatal("delivery cursor stalled")
			}
			offset, digest = p.NextSnapshotOffset, p.SnapshotDigest
		}
		if len(seen) != 100 {
			t.Fatalf("%s records=%d", method, len(seen))
		}
	}
}
