package stdioworker

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Child is the worker-side (in-sandbox) half of the 5B contract. A real
// worker embeds it: it speaks HELLO/HEARTBEAT/RESULT on stdout frames with
// the session binding the host assigned via environment variables.
type Child struct {
	sessionID  string
	specDigest string
	out        io.Writer
	seq        int64
}

// ChildFromEnv builds the Child from the environment the runtime provides.
// Inside the sandbox ONLY these variables exist (explicit environment
// block), so a missing binding is a protocol violation from the start.
func ChildFromEnv(getenv func(string) string, out io.Writer) (*Child, error) {
	sid := getenv("STDIOWORKER_SESSION")
	digest := getenv("STDIOWORKER_SPEC_DIGEST")
	if sid == "" || digest == "" {
		return nil, fmt.Errorf("stdioworker child: session binding missing")
	}
	return &Child{sessionID: sid, specDigest: digest, out: out}, nil
}

// SessionID returns the bound session.
func (c *Child) SessionID() string { return c.sessionID }

func (c *Child) send(typ string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	env := Envelope{SessionID: c.sessionID, Seq: c.seq, Type: typ, Data: raw}
	if err := WriteEnvelope(c.out, &env); err != nil {
		return err
	}
	c.seq++
	return nil
}

// Hello sends the launch handshake binding the spec digest and protocol
// version. The host kills the session on mismatch.
func (c *Child) Hello() error {
	return c.send(EnvHello, map[string]string{
		"specDigest": c.specDigest,
		"protocol":   DefaultPolicy().ProtocolVer,
	})
}

// Heartbeat sends one liveness beat.
func (c *Child) Heartbeat() error {
	return c.send(EnvHeartbeat, map[string]int64{"at": time.Now().UnixMilli()})
}

// Result submits the final payload and ends the run.
func (c *Child) Result(v any) error {
	return c.send(EnvResult, v)
}

// Job reads one host→child job envelope from r.
func Job(r io.Reader) (*Envelope, error) { return ReadEnvelope(r) }
