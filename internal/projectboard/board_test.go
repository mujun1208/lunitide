package projectboard

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projecttask"
)

func TestSyncCountsAddedModifiedRemoved(t *testing.T) {
	src := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "I001", Title: "GET /a", Method: "GET", Path: "/a"},
		{ID: "I002", Title: "POST /b", Method: "POST", Path: "/b"},
	}}
	board := Sync(projecttask.Doc{Version: 1}, src)
	st := Summarize(board)
	if st.Total != 2 || st.Added != 2 || st.NeedsReprocess != 2 {
		t.Fatalf("first sync %+v items=%+v", st, board.Items)
	}
	if !strings.Contains(FormatStats(st), "共 2 条") || !strings.Contains(FormatStats(st), "新增 2") {
		t.Fatalf("copy %s", FormatStats(st))
	}
	board.Items[0].NeedsReprocess = false
	board.Items[0].ChangeKind = "unchanged"
	board.Items[0].Status = "dev_done"
	src.Items[0].Title = "GET /a 改名"
	src.Items = src.Items[:1]
	board = Sync(board, src)
	st = Summarize(board)
	if st.Modified != 1 || st.Removed != 1 || !Dirty(board) {
		t.Fatalf("second %+v items=%+v", st, board.Items)
	}
}

func TestIntegrationReadyAndOpenAPI(t *testing.T) {
	tests := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "T-I001", Status: "test_pass"}}}
	scene := projecttask.Item{ID: "S01", MemberIDs: []string{"T-I001"}}
	if err := IntegrationReady(scene, tests); err != nil {
		t.Fatal(err)
	}
	scene.MemberIDs = []string{"T-I001", "T-MISSING"}
	if err := IntegrationReady(scene, tests); err != projectapp.ErrIntegrationNotReady {
		t.Fatalf("got %v", err)
	}
	doc, err := ParseOpenAPI([]byte(`{"openapi":"3.0.3","paths":{"/health":{"get":{"operationId":"getHealth","summary":"健康"}}}}`))
	if err != nil || len(doc.Items) != 1 || doc.Items[0].Method != "GET" || doc.Items[0].ID != "I001" {
		t.Fatalf("%+v %v", doc, err)
	}
	multi, err := ParseOpenAPI([]byte(`{"openapi":"3.0.3","paths":{"/z":{"get":{"summary":"后"}},"/a":{"post":{"summary":"先"}}}}`))
	if err != nil || len(multi.Items) != 2 || multi.Items[0].Path != "/a" || multi.Items[0].ID != "I001" || multi.Items[1].Path != "/z" {
		t.Fatalf("stable ids %+v %v", multi.Items, err)
	}
}

func TestBuildTestBoardDualSource(t *testing.T) {
	out := BuildTestBoard(
		projecttask.Doc{Items: []projecttask.Item{{ID: "I001", Title: "健康", Status: "dev_done"}}},
		projecttask.Doc{Items: []projecttask.Item{{ID: "F001", Title: "登录", Status: "dev_done"}}},
	)
	if len(out.Items) != 2 || out.Items[0].SourceKind != "interface" || out.Items[1].SourceKind != "dev" {
		t.Fatalf("%+v", out.Items)
	}
}
