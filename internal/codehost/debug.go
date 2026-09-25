package codehost

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dapMsg struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success,omitempty"`
	Event      string          `json:"event,omitempty"`
	Message    string          `json:"message,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

type debugger struct {
	cmd    *exec.Cmd
	conn   net.Conn
	br     *bufio.Reader
	mu     sync.Mutex
	wmu    sync.Mutex
	seq    int
	wait   map[int]chan dapMsg
	events chan dapMsg
}

// StopOnLine builds the test package and runs dlv until the breakpoint line is hit.
func StopOnLine(root, file string, line int, testName string) (int, error) {
	bin, err := toolBin("dlv")
	if err != nil {
		return 0, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	cmd := exec.Command(bin, "dap", "--client-addr", ln.Addr().String(), "--check-go-version=false")
	cmd.Dir = root
	cmd.Env = toolEnv()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return 0, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(20 * time.Second))
	conn, err := ln.Accept()
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	d := &debugger{cmd: cmd, conn: conn, br: bufio.NewReader(conn), wait: map[int]chan dapMsg{}, events: make(chan dapMsg, 32)}
	go d.read()
	if _, err = d.request("initialize", map[string]any{
		"clientID": "lunitide", "adapterID": "go", "pathFormat": "path",
		"linesStartAt1": true, "columnsStartAt1": true,
	}, 20*time.Second); err != nil {
		return 0, err
	}
	args := []string{}
	if testName != "" {
		args = []string{"-test.run", "^" + testName + "$"}
	}
	if _, err = d.request("launch", map[string]any{
		"request": "launch", "mode": "test", "program": root,
		"args": args, "stopOnEntry": false,
	}, 60*time.Second); err != nil {
		return 0, err
	}
	if _, err = d.request("setBreakpoints", map[string]any{
		"source":      map[string]any{"path": file},
		"breakpoints": []any{map[string]any{"line": line}},
	}, 20*time.Second); err != nil {
		return 0, err
	}
	if _, err = d.request("configurationDone", map[string]any{}, 20*time.Second); err != nil {
		return 0, err
	}
	stopped, err := d.until("stopped", 60*time.Second)
	if err != nil {
		return 0, err
	}
	var body struct {
		ThreadID int `json:"threadId"`
	}
	_ = json.Unmarshal(stopped.Body, &body)
	if body.ThreadID == 0 {
		body.ThreadID = 1
	}
	stack, err := d.request("stackTrace", map[string]any{"threadId": body.ThreadID, "startFrame": 0, "levels": 5}, 20*time.Second)
	if err != nil {
		return 0, err
	}
	var frames struct {
		StackFrames []struct {
			Line int `json:"line"`
		} `json:"stackFrames"`
	}
	if json.Unmarshal(stack.Body, &frames) != nil || len(frames.StackFrames) == 0 {
		return 0, errors.New("no stack frame")
	}
	_, _ = d.request("disconnect", map[string]any{"terminateDebuggee": true}, 5*time.Second)
	return frames.StackFrames[0].Line, nil
}

func (d *debugger) request(command string, args any, wait time.Duration) (dapMsg, error) {
	d.mu.Lock()
	d.seq++
	seq := d.seq
	ch := make(chan dapMsg, 1)
	d.wait[seq] = ch
	d.mu.Unlock()
	raw, _ := json.Marshal(args)
	msg := dapMsg{Seq: seq, Type: "request", Command: command, Arguments: raw}
	if err := d.write(msg); err != nil {
		return dapMsg{}, err
	}
	select {
	case got := <-ch:
		if !got.Success {
			if got.Message == "" {
				got.Message = command + " failed"
			}
			return got, errors.New(got.Message)
		}
		return got, nil
	case <-time.After(wait):
		return dapMsg{}, fmt.Errorf("%s timed out", command)
	}
}

func (d *debugger) until(event string, wait time.Duration) (dapMsg, error) {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		select {
		case msg := <-d.events:
			if msg.Event == event {
				return msg, nil
			}
		case <-deadline.C:
			return dapMsg{}, fmt.Errorf("event %s timed out", event)
		}
	}
}

func (d *debugger) write(msg dapMsg) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	d.wmu.Lock()
	defer d.wmu.Unlock()
	_, err = fmt.Fprintf(d.conn, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

func (d *debugger) read() {
	for {
		var length int
		for {
			line, err := d.br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(strings.ToLower(line), "content-length:") {
				length, _ = strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			}
		}
		if length <= 0 {
			continue
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(d.br, buf); err != nil {
			return
		}
		var msg dapMsg
		if json.Unmarshal(buf, &msg) != nil {
			continue
		}
		if msg.Type == "response" {
			d.mu.Lock()
			ch := d.wait[msg.RequestSeq]
			delete(d.wait, msg.RequestSeq)
			d.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		if msg.Type == "event" {
			select {
			case d.events <- msg:
			default:
			}
		}
	}
}
