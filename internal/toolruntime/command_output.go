package toolruntime

import (
	"bytes"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const commandOutputLimit = 64 << 10
const commandProgressBufferLimit = 16 << 10
const commandProgressFlushTimeout = 50 * time.Millisecond

// External progress sinks have no cancellation contract. A stuck sink can hold
// one slot forever, so bound these calls across the process, not per command.
// While occupied, another command may lose progress; its final output is kept.
var commandProgressSlot = make(chan struct{}, 1)

// commandOutput drains stdout and stderr even after the retained output budget
// is exhausted. Both execution modes share this bounded collector, so a single
// long line cannot stop pipe consumption or grow the engine's memory without bound.
type commandOutput struct {
	mu        sync.Mutex
	retained  []byte
	line      []byte
	truncated bool
	progress  func(string)
	emitted   int
	queue     chan string
	stop      chan struct{}
	done      chan struct{}
	closed    bool
}

func (b *commandOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	keep := min(n, commandOutputLimit-len(b.retained))
	b.retained = append(b.retained, p[:keep]...)
	b.truncated = b.truncated || keep < n
	for len(p) > 0 && b.progress != nil && !b.closed && b.emitted < toolProgressMaxChunks {
		take := min(len(p), commandProgressBufferLimit-len(b.line))
		if nl := bytes.IndexByte(p[:take], '\n'); nl >= 0 {
			take = nl + 1
		}
		b.line = append(b.line, p[:take]...)
		p = p[take:]
		if len(b.line) == commandProgressBufferLimit || b.line[len(b.line)-1] == '\n' {
			b.emitLine(len(b.line) == commandProgressBufferLimit)
		}
	}
	return n, nil
}

func (b *commandOutput) emitLine(split bool) {
	if len(b.line) == 0 {
		return
	}
	end := len(b.line)
	if split {
		end = completeUTF8Prefix(b.line)
	}
	if b.progress != nil && !b.closed && b.emitted < toolProgressMaxChunks {
		if b.queue == nil {
			b.queue = make(chan string, toolProgressMaxChunks)
			b.stop, b.done = make(chan struct{}), make(chan struct{})
			go b.runProgress()
		}
		chunk := truncateRunes(strings.TrimRight(decodeCommandOutput(b.line[:end]), "\r\n"), 400)
		select {
		case b.queue <- chunk:
		default: // Progress is best effort and must never block pipe drainage.
		}
		b.emitted++
	}
	b.line = append(b.line[:0], b.line[end:]...)
}

func (b *commandOutput) runProgress() {
	defer close(b.done)
	for {
		select {
		case <-b.stop:
			return
		case chunk, open := <-b.queue:
			if !open {
				return
			}
			select {
			case <-b.stop:
				return
			default:
			}
			select {
			case commandProgressSlot <- struct{}{}:
			default:
				continue
			}
			// A sink panic must not kill the engine from this detached worker.
			ok := b.deliverProgress(chunk)
			<-commandProgressSlot
			if !ok {
				return
			}
		}
	}
}

func (b *commandOutput) deliverProgress(chunk string) (ok bool) {
	defer func() { _ = recover() }()
	select {
	case <-b.stop:
		return false
	default:
	}
	b.progress(chunk)
	return true
}

// closeProgress allows prompt callbacks to flush, then discards pending work
// after a fixed grace period. One blocked callback may return later.
func (b *commandOutput) closeProgress() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	if b.queue == nil {
		b.mu.Unlock()
		return
	}
	close(b.queue)
	b.mu.Unlock()
	timer := time.NewTimer(commandProgressFlushTimeout)
	defer timer.Stop()
	select {
	case <-b.done:
	case <-timer.C:
	}
	close(b.stop)
}

// Preserve an incomplete UTF-8 suffix at an artificial byte limit. Only trim
// when the preceding bytes are valid UTF-8; GB18030 decoding stays available.
func completeUTF8Prefix(raw []byte) int {
	if utf8.Valid(raw) {
		return len(raw)
	}
	for start := len(raw) - 1; start >= 0 && start >= len(raw)-(utf8.UTFMax-1); start-- {
		if utf8.RuneStart(raw[start]) {
			if !utf8.FullRune(raw[start:]) && utf8.Valid(raw[:start]) {
				return start
			}
			break
		}
	}
	return len(raw)
}

func (b *commandOutput) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.emitLine(false)
	end := len(b.retained)
	if b.truncated {
		end = completeUTF8Prefix(b.retained)
	}
	text := decodeCommandOutput(b.retained[:end])
	if b.truncated {
		text += "\n[output truncated at 64 KiB; process output was drained]"
	}
	return text
}
