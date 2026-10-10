package toolruntime

import (
	"context"
	"os"
	"strings"
	"testing"
)

func canvasArgs(title, body string) []byte {
	return []byte(`{"title":"` + title + `","sections":[{"heading":"正文","body":"` + body + `"}]}`)
}

func readSessionArtifact(t *testing.T, r *Runtime, session, rel string) string {
	t.Helper()
	p, err := r.sessionArtifactFile(session, rel)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCanvasPresentWritesADocumentTheWorkspaceCanShow(t *testing.T) {
	r := newProductRuntime(t)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := []byte(`{"title":"能力对照","intro":"落地后的文档","sections":[{"heading":"结论","body":"画布可以看"}],"bars":[{"label":"编辑器","value":8,"max":10}],"html":"<p>补充</p><script>alert(1)</script>"}`)
	out, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", args, true)
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifact == nil || out.Artifact.Kind != "html" || out.Artifact.Path != "能力对照.html" {
		t.Fatalf("%+v", out.Artifact)
	}
	page := out.Artifact.Content
	if !strings.Contains(page, "能力对照") || !strings.Contains(page, "画布可以看") || !strings.Contains(page, "width:80.0%") {
		t.Fatalf("document missing the report: %s", page)
	}
	if strings.Contains(strings.ToLower(page), "<script") {
		t.Fatal("canvas kept a script")
	}
	if got := readSessionArtifact(t, r, session, "能力对照.html"); !strings.Contains(got, "能力对照") {
		t.Fatal("title-named canvas file was not written")
	}
	// canvas.html stays the latest-canvas mirror the workspace canvas tab previews.
	if got := readSessionArtifact(t, r, session, "canvas.html"); !strings.Contains(got, "能力对照") {
		t.Fatal("canvas.html mirror was not written")
	}
}

// 同一对话两份不同标题的画布产物必须各占一个文件，互不覆盖。
func TestCanvasPresentKeepsBothDeliverablesInTheSameSession(t *testing.T) {
	r := newProductRuntime(t)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	first, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", canvasArgs("星座爱情匹配分析", "白羊与狮子"), true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", canvasArgs("哲学文学科学对比", "三者的边界"), true)
	if err != nil {
		t.Fatal(err)
	}
	if first.Artifact.Path == second.Artifact.Path {
		t.Fatalf("two distinct deliverables share one path: %s", first.Artifact.Path)
	}
	if got := readSessionArtifact(t, r, session, first.Artifact.Path); !strings.Contains(got, "白羊与狮子") || strings.Contains(got, "三者的边界") {
		t.Fatal("the first deliverable was overwritten by the second")
	}
	if got := readSessionArtifact(t, r, session, second.Artifact.Path); !strings.Contains(got, "三者的边界") {
		t.Fatal("the second deliverable was not written")
	}
	// 镜像始终是最新一版。
	if got := readSessionArtifact(t, r, session, "canvas.html"); !strings.Contains(got, "三者的边界") {
		t.Fatal("canvas.html mirror does not hold the latest canvas")
	}
}

// 同标题不同内容：第二版改写到 <标题>-2.html，第一版原地保留。
func TestCanvasPresentStepsAsideWhenTheTitleCollides(t *testing.T) {
	r := newProductRuntime(t)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	first, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", canvasArgs("调研报告", "初稿内容"), true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", canvasArgs("调研报告", "修改后的内容"), true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Artifact.Path != "调研报告-2.html" {
		t.Fatalf("expected 调研报告-2.html, got %s", second.Artifact.Path)
	}
	if got := readSessionArtifact(t, r, session, first.Artifact.Path); !strings.Contains(got, "初稿内容") {
		t.Fatal("same-titled first version was overwritten")
	}
	if got := readSessionArtifact(t, r, session, second.Artifact.Path); !strings.Contains(got, "修改后的内容") {
		t.Fatal("same-titled second version was not written")
	}
}

// 完全相同的内容重复呈现：幂等复用同一文件，不产生 -2 副本。
func TestCanvasPresentReusesThePathForIdenticalContent(t *testing.T) {
	r := newProductRuntime(t)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := canvasArgs("同一份报告", "不变的内容")
	first, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", args, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", args, true)
	if err != nil {
		t.Fatal(err)
	}
	if first.Artifact.Path != second.Artifact.Path {
		t.Fatalf("identical content forked into %s and %s", first.Artifact.Path, second.Artifact.Path)
	}
}

// 标题净化后不可用时回落 canvas-report.html，永不占用镜像名；
// 第二份不同内容得到 -2，第一份与镜像互不干扰。
func TestCanvasPresentUnusableTitleFallsBackWithoutClobbering(t *testing.T) {
	r := newProductRuntime(t)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	first, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", canvasArgs("///", "第一版"), true)
	if err != nil {
		t.Fatal(err)
	}
	if first.Artifact.Path != "canvas-report.html" {
		t.Fatalf("fallback path = %s", first.Artifact.Path)
	}
	second, err := r.Execute(context.Background(), FullAccess, session, "canvas.present", canvasArgs("???", "第二版"), true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Artifact.Path != "canvas-report-2.html" {
		t.Fatalf("collision path = %s", second.Artifact.Path)
	}
	if got := readSessionArtifact(t, r, session, "canvas-report.html"); !strings.Contains(got, "第一版") {
		t.Fatal("the unusable-titled first deliverable was overwritten")
	}
	if got := readSessionArtifact(t, r, session, "canvas.html"); !strings.Contains(got, "第二版") {
		t.Fatal("the mirror does not track the latest canvas")
	}
}

func TestSanitizeTitleFileName(t *testing.T) {
	cases := map[string]string{
		"能力对照":                  "能力对照",
		`报告<"A:/B|C?D*>`:        "报告ABCD",
		"  多  空格　标题  ":          "多 空格 标题",
		"结尾点号...":               "结尾点号",
		"///":                   "",
		"":                      "",
		strings.Repeat("长", 60): strings.Repeat("长", 40),
	}
	for title, want := range cases {
		if got := sanitizeTitleFileName(title); got != want {
			t.Fatalf("sanitizeTitleFileName(%q) = %q, want %q", title, got, want)
		}
	}
}
