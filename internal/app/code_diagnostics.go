package app

import (
	"strings"
	"sync"
)

type codeDiagnosticState struct {
	mu    sync.Mutex
	notes map[string]string
}

func (e *Engine) rememberCodeDiagnostic(session, text string) {
	text = strings.TrimSpace(text)
	if e == nil || session == "" || text == "" {
		return
	}
	if e.codeNotes == nil {
		e.codeNotes = &codeDiagnosticState{notes: map[string]string{}}
	}
	e.codeNotes.mu.Lock()
	defer e.codeNotes.mu.Unlock()
	if e.codeNotes.notes == nil {
		e.codeNotes.notes = map[string]string{}
	}
	e.codeNotes.notes[session] = text
}

func (e *Engine) codeDiagnosticInjection(session string) string {
	if e == nil || e.codeNotes == nil || session == "" {
		return ""
	}
	e.codeNotes.mu.Lock()
	text := strings.TrimSpace(e.codeNotes.notes[session])
	e.codeNotes.mu.Unlock()
	if text == "" {
		return ""
	}
	if len(text) > 4000 {
		text = text[:4000]
	}
	return "\n\n[代码诊断]\n" + text + "\n引用诊断里的文件名和行号。不要整文件重写。\n"
}
