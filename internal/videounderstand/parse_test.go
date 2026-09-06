package videounderstand

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHTMLFixtures(t *testing.T) {
	t.Parallel()
	og := ParseHTML(readTestdata(t, "og_only.html"))
	if og.Title != "月食观测指南" || !strings.Contains(og.Description, "月食") {
		t.Fatalf("og page = %+v", og)
	}
	if og.LoginWall || og.Captcha {
		t.Fatal("og page must not be a wall")
	}

	cap := ParseHTML(readTestdata(t, "captions_bilibili.html"))
	if cap.Title != "公开字幕样例" || cap.Author != "UP主甲" {
		t.Fatalf("bilibili state = %+v", cap)
	}
	if !strings.Contains(cap.CaptionURL, "aisubtitle.hdslb.com") {
		t.Fatalf("caption url = %q", cap.CaptionURL)
	}

	empty := ParseHTML(readTestdata(t, "empty_spa.html"))
	if empty.Title != "" || empty.Description != "" {
		t.Fatalf("empty spa leaked meta: %+v", empty)
	}

	wall := ParseHTML(readTestdata(t, "login_wall.html"))
	if !wall.LoginWall {
		t.Fatalf("login wall not detected: %+v", wall)
	}
	titled := ParseHTML(`<title>登录 - 抖音</title><p>请登录后继续观看</p>`)
	if !titled.LoginWall {
		t.Fatalf("titled login wall not detected: %+v", titled)
	}
}

func TestParseCaptionsJSON(t *testing.T) {
	t.Parallel()
	text, trunc := ParseCaptions("application/json", []byte(`[{"content":"第一句"},{"content":"第二句"}]`))
	if trunc || text != "第一句\n第二句" {
		t.Fatalf("captions=%q trunc=%v", text, trunc)
	}
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
