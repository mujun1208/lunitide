package mediahost

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
)

type Player struct {
	Engine           EngineCaller
	WindowInstanceID string
	NavigationEpoch  int64
	RenewEvery       time.Duration

	mu         sync.Mutex
	sessionID  string
	leaseToken string
	generation int64
	operation  string
}

func (p *Player) window() string {
	if p == nil || strings.TrimSpace(p.WindowInstanceID) == "" {
		return "desktop-main"
	}
	return p.WindowInstanceID
}

func (p *Player) Attach(ctx context.Context, sessionID string) error {
	if p == nil || p.Engine == nil || !validPlayerULID(sessionID) {
		return fmt.Errorf("player attach unavailable")
	}
	payload, _ := json.Marshal(map[string]any{
		"mediaSessionId":   sessionID,
		"windowInstanceId": p.window(),
		"navigationEpoch":  p.NavigationEpoch,
	})
	resp, err := p.Engine.Call(ctx, bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: "internal.media.player.attach", Payload: payload, DeadlineMS: 8000,
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("player attach refused")
	}
	raw, _ := json.Marshal(resp.Payload)
	var out struct {
		LeaseToken string `json:"leaseToken"`
		Generation int64  `json:"generation"`
	}
	if json.Unmarshal(raw, &out) != nil || out.LeaseToken == "" {
		return fmt.Errorf("player attach refused")
	}
	p.mu.Lock()
	p.sessionID = sessionID
	p.leaseToken = out.LeaseToken
	p.generation = out.Generation
	p.mu.Unlock()
	return nil
}

func (p *Player) Next(ctx context.Context) (operationID, desiredState string, err error) {
	sessionID, token, generation, err := p.lease()
	if err != nil {
		return "", "", err
	}
	payload, _ := json.Marshal(map[string]any{
		"mediaSessionId":   sessionID,
		"leaseToken":       token,
		"generation":       generation,
		"windowInstanceId": p.window(),
		"navigationEpoch":  p.NavigationEpoch,
	})
	resp, err := p.Engine.Call(ctx, bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: "internal.media.player.next", Payload: payload, DeadlineMS: 8000,
	})
	if err != nil {
		return "", "", err
	}
	if !resp.OK {
		return "", "", fmt.Errorf("player next refused")
	}
	raw, _ := json.Marshal(resp.Payload)
	var out struct {
		OperationID  *string `json:"operationId"`
		DesiredState *string `json:"desiredState"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return "", "", fmt.Errorf("player next refused")
	}
	if out.OperationID == nil || *out.OperationID == "" {
		p.mu.Lock()
		p.operation = ""
		p.mu.Unlock()
		return "", "", nil
	}
	if out.DesiredState != nil {
		desiredState = *out.DesiredState
	}
	p.mu.Lock()
	p.operation = *out.OperationID
	p.mu.Unlock()
	return *out.OperationID, desiredState, nil
}

func (p *Player) ReportObserved(ctx context.Context, event string, positionMs, durationMs int64) error {
	switch event {
	case "playing", "pause", "ended", "stalled", "error", "position":
	default:
		return fmt.Errorf("player report event invalid")
	}
	sessionID, token, generation, err := p.lease()
	if err != nil {
		return err
	}
	p.mu.Lock()
	op := p.operation
	p.mu.Unlock()
	if !validPlayerULID(op) {
		return fmt.Errorf("player report missing command")
	}
	payload, _ := json.Marshal(map[string]any{
		"mediaSessionId":   sessionID,
		"leaseToken":       token,
		"generation":       generation,
		"windowInstanceId": p.window(),
		"navigationEpoch":  p.NavigationEpoch,
		"operationId":      op,
		"event":            event,
		"positionMs":       positionMs,
		"durationMs":       durationMs,
	})
	resp, err := p.Engine.Call(ctx, bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: "internal.media.player.report", Payload: payload, DeadlineMS: 8000,
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("player report refused")
	}
	return nil
}

func (p *Player) Observe(ctx context.Context, sessionID, event string, positionMs, durationMs int64) (bool, error) {
	switch event {
	case "playing", "pause", "ended", "stalled", "error", "position":
	default:
		return false, fmt.Errorf("player report event invalid")
	}
	if err := p.Attach(ctx, sessionID); err != nil {
		return false, err
	}
	if event != "position" {
		if _, _, err := p.Next(ctx); err != nil {
			return false, err
		}
	}
	p.mu.Lock()
	op := p.operation
	p.mu.Unlock()
	if !validPlayerULID(op) {
		return false, nil
	}
	if err := p.ReportObserved(ctx, event, positionMs, durationMs); err != nil {
		return false, err
	}
	return true, nil
}

func (p *Player) lease() (sessionID, token string, generation int64, err error) {
	if p == nil || p.Engine == nil {
		return "", "", 0, fmt.Errorf("player attach unavailable")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessionID == "" || p.leaseToken == "" {
		return "", "", 0, fmt.Errorf("player attach unavailable")
	}
	return p.sessionID, p.leaseToken, p.generation, nil
}

func (p *Player) StartRenew(ctx context.Context) {
	if p == nil {
		return
	}
	every := p.RenewEvery
	if every <= 0 {
		every = 10 * time.Second
	}
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.mu.Lock()
				session := p.sessionID
				p.mu.Unlock()
				if session != "" {
					_ = p.Attach(ctx, session)
				}
			}
		}
	}()
}

func validPlayerULID(id string) bool {
	return len(id) == 26
}

func RunLease(ctx context.Context, player *Player, events <-chan bridge.Event) {
	if player == nil || events == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	var session string
	attach := func() {
		if session == "" {
			return
		}
		_ = player.Attach(ctx, session)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type != bridge.EventMediaSnapshot || ev.Media == nil || !validPlayerULID(ev.Media.MediaSessionID) {
				continue
			}
			session = ev.Media.MediaSessionID
			attach()
		case <-ticker.C:
			attach()
		}
	}
}
