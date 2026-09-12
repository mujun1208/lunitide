package agenthub

import (
	"bufio"
	"os"
	"testing"
)

func TestParseFixtureLines(t *testing.T) {
	file, err := os.Open("testdata/codex-hello.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var types []string
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		ev, ok := ParseLine("codex", sc.Text())
		if !ok {
			t.Fatalf("dropped %q", sc.Text())
		}
		types = append(types, ev.Type)
	}
	if err = sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(types) < 3 || types[0] != "started" {
		t.Fatalf("%v", types)
	}
}

func TestParseKimiAssistantContentArray(t *testing.T) {
	ev, ok := ParseLine("kimi", `{"type":"assistant","message":{"content":[{"type":"text","text":"created hello.txt"}]}}`)
	if !ok || ev.Detail != "created hello.txt" {
		t.Fatalf("%+v %v", ev, ok)
	}
}

func TestParseKimiStreamJSONRoleAssistant(t *testing.T) {
	ev, ok := ParseLine("kimi", `{"role":"assistant","content":"ok"}`)
	if !ok || ev.Type != "message" || ev.Detail != "ok" {
		t.Fatalf("%+v %v", ev, ok)
	}
}

func TestParseCursorAssistantContentArray(t *testing.T) {
	ev, ok := ParseLine("cursor", `{"type":"assistant","message":{"content":[{"type":"text","text":"writing hello.txt"}]}}`)
	if !ok || ev.Detail != "writing hello.txt" {
		t.Fatalf("%+v %v", ev, ok)
	}
}

func TestParseGarbageIsMessage(t *testing.T) {
	ev, ok := ParseLine("codex", "not-json {{{")
	if !ok || ev.Type != "message" {
		t.Fatalf("%+v %v", ev, ok)
	}
}

func TestParseMissingFields(t *testing.T) {
	ev, ok := ParseLine("cursor", `{"type":"step"}`)
	if !ok || ev.Type != "step" || ev.Title == "" {
		t.Fatalf("%+v", ev)
	}
}

func TestParseUsageTokens(t *testing.T) {
	ev, _ := ParseLine("codex", `{"type":"usage","usage":{"input_tokens":2,"output_tokens":3}}`)
	if ev.Tokens != 5 {
		t.Fatalf("%+v", ev)
	}
	ev, _ = ParseLine("codex", `{"type":"usage"}`)
	if ev.Tokens != 0 {
		t.Fatalf("%+v", ev)
	}
}
