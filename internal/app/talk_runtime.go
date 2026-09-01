package app

import (
	"context"
	"net/http"
	"sync"

	"github.com/lunitide/lunitide/internal/talk"
	"github.com/oklog/ulid/v2"
)

type talkSession struct {
	talkID    string
	streamID  string
	sessionID string
	conn      talk.Conn
	writeMu   sync.Mutex
	cancel    context.CancelFunc
	once      sync.Once
}

func (s *talkSession) write(raw []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.conn == nil {
		return errTalkClosed
	}
	return talk.WriteText(s.conn, raw)
}

func (s *talkSession) shutdown() {
	s.once.Do(func() {
		s.writeMu.Lock()
		if s.conn != nil {
			_ = s.conn.Close()
			s.conn = nil
		}
		s.writeMu.Unlock()
		if s.cancel != nil {
			s.cancel()
		}
	})
}

func (e *Engine) talkDial(ctx context.Context, rawURL string, header http.Header) (talk.Conn, error) {
	dial := e.talkDialer
	if dial == nil {
		dial = talk.DefaultDialer
	}
	return dial(ctx, rawURL, header)
}

func (e *Engine) putTalk(session *talkSession) {
	e.talkMu.Lock()
	defer e.talkMu.Unlock()
	if e.talks == nil {
		e.talks = make(map[string]*talkSession)
	}
	if prior, ok := e.talks[session.sessionID]; ok && prior != session {
		prior.shutdown()
	}
	e.talks[session.sessionID] = session
}

func (e *Engine) talkBySession(sessionID string) *talkSession {
	e.talkMu.Lock()
	defer e.talkMu.Unlock()
	if e.talks == nil {
		return nil
	}
	return e.talks[sessionID]
}

func (e *Engine) dropTalk(sessionID, talkID string) {
	e.talkMu.Lock()
	defer e.talkMu.Unlock()
	current, ok := e.talks[sessionID]
	if !ok || (talkID != "" && current.talkID != talkID) {
		return
	}
	delete(e.talks, sessionID)
	current.shutdown()
}

func newTalkIDs() (talkID, streamID string) {
	return ulid.Make().String(), ulid.Make().String()
}
