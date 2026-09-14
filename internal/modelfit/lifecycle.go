package modelfit

import (
	"errors"
	"sync"
	"time"
)

const (
	EvidenceLive    = "live"
	EvidenceFixture = "fixture"
	EvidenceMock    = "mock"
)

var (
	ErrFixtureCannotPromote = errors.New("MODEL_FIXTURE_CANNOT_PROMOTE")
	ErrScopeIncomplete      = errors.New("MODEL_QUALIFICATION_SCOPE_INCOMPLETE")
	ErrQualificationExpired = errors.New("MODEL_QUALIFICATION_EXPIRED")
	ErrSourceDigestChanged  = errors.New("MODEL_SOURCE_DIGEST_CHANGED")
	ErrBindingConflict      = errors.New("MODEL_BINDING_CONFLICT")
	ErrBindingUnavailable   = errors.New("MODEL_BINDING_UNAVAILABLE")
)

type QualificationScope struct {
	TargetDigest  string
	ProfileID     string
	ProfileDigest string
	SuiteID       string
	AppVersion    string
	Renderer      string
}

func (s QualificationScope) Complete() bool {
	return s.TargetDigest != "" && s.ProfileID != "" && s.ProfileDigest != "" && s.SuiteID != "" && s.AppVersion != "" && s.Renderer != ""
}

type InFlightTask struct {
	TaskID   string
	Budget   []byte
	Progress []byte
}

type ActivateInput struct {
	OwnerScope        string
	ProviderID        string
	Family            string
	ModelID           string
	CodecVersion      string
	BindingID         string
	QualificationID   string
	ExpectedRevision  int64
	IdempotencyKey    string
	EvidenceKind      string
	Scope             QualificationScope
	BoundScope        QualificationScope
	ExpiresAt         time.Time
	Now               time.Time
	SourceDigest      string
	BoundSourceDigest string
	FixtureRow        *Qualification
	InFlight          InFlightTask
}

type ActiveBinding struct {
	OwnerScope        string
	ProviderID        string
	ModelID           string
	BindingID         string
	QualificationID   string
	PreviousBindingID string
	Revision          int64
}

type ActivateResult struct {
	ActiveBinding   ActiveBinding
	PreviousBinding *ActiveBinding
	InFlight        InFlightTask
}

type BindingLedger struct {
	mu     sync.Mutex
	slots  map[string]ActiveBinding
	prev   map[string]ActiveBinding
	idem   map[string]ActivateResult
	flight map[string]InFlightTask
}

func NewBindingLedger() *BindingLedger {
	return &BindingLedger{
		slots:  map[string]ActiveBinding{},
		prev:   map[string]ActiveBinding{},
		idem:   map[string]ActivateResult{},
		flight: map[string]InFlightTask{},
	}
}

func bindingKey(ownerScope, providerID, modelID string) string {
	return ownerScope + "\x1f" + providerID + "\x1f" + modelID
}

func RevalidateAfterSourceChange(q Qualification, boundDigest, currentDigest string) Qualification {
	if boundDigest == currentDigest {
		return q
	}
	q.Status = QualifyBlocked
	q.Adopted = false
	return q
}

func DecideActivate(in ActivateInput) error {
	if in.FixtureRow != nil {
		switch in.FixtureRow.Status {
		case QualifyUntested, QualifyFixturePass, QualifyBlocked, "qualified":
			return ErrFixtureCannotPromote
		}
	}
	if in.EvidenceKind != EvidenceLive {
		return ErrFixtureCannotPromote
	}
	if !in.Scope.Complete() {
		return ErrScopeIncomplete
	}
	if in.ExpiresAt.IsZero() || !in.Now.Before(in.ExpiresAt) {
		return ErrQualificationExpired
	}
	if in.SourceDigest != in.BoundSourceDigest || in.Scope.ProfileDigest != in.BoundScope.ProfileDigest || in.Scope.TargetDigest != in.BoundScope.TargetDigest {
		return ErrSourceDigestChanged
	}
	return nil
}

func (l *BindingLedger) Get(ownerScope, providerID, modelID string) (ActiveBinding, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	got, ok := l.slots[bindingKey(ownerScope, providerID, modelID)]
	return got, ok
}

func (l *BindingLedger) GetInFlight(ownerScope, providerID, modelID string) (InFlightTask, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	got, ok := l.flight[bindingKey(ownerScope, providerID, modelID)]
	return cloneInFlight(got), ok
}

func (l *BindingLedger) Activate(in ActivateInput) (ActivateResult, error) {
	if err := DecideActivate(in); err != nil {
		return ActivateResult{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if in.IdempotencyKey != "" {
		if prev, ok := l.idem[idempotencyKey(in)]; ok {
			return prev, nil
		}
	}
	key := bindingKey(in.OwnerScope, in.ProviderID, in.ModelID)
	current, ok := l.slots[key]
	var have int64
	if ok {
		have = current.Revision
	}
	if in.ExpectedRevision != have {
		return ActivateResult{}, ErrBindingConflict
	}
	next := ActiveBinding{
		OwnerScope:      in.OwnerScope,
		ProviderID:      in.ProviderID,
		ModelID:         in.ModelID,
		BindingID:       in.BindingID,
		QualificationID: in.QualificationID,
		Revision:        have + 1,
	}
	var previous *ActiveBinding
	if ok {
		kept := current
		previous = &kept
		next.PreviousBindingID = current.BindingID
		l.prev[key] = current
	}
	l.slots[key] = next
	out := ActivateResult{ActiveBinding: next, PreviousBinding: previous, InFlight: l.bindInFlight(key, in.InFlight)}
	if in.IdempotencyKey != "" {
		l.idem[idempotencyKey(in)] = out
	}
	return out, nil
}

func (l *BindingLedger) Rollback(in ActivateInput) (ActivateResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := bindingKey(in.OwnerScope, in.ProviderID, in.ModelID)
	current, ok := l.slots[key]
	if !ok {
		return ActivateResult{}, ErrBindingUnavailable
	}
	if in.ExpectedRevision != current.Revision {
		return ActivateResult{}, ErrBindingConflict
	}
	prev, ok := l.prev[key]
	if !ok || current.PreviousBindingID == "" {
		return ActivateResult{}, ErrBindingUnavailable
	}
	kept := current
	l.slots[key] = prev
	return ActivateResult{ActiveBinding: prev, PreviousBinding: &kept, InFlight: cloneInFlight(l.flight[key])}, nil
}

func (l *BindingLedger) bindInFlight(key string, in InFlightTask) InFlightTask {
	if stored, ok := l.flight[key]; ok {
		return cloneInFlight(stored)
	}
	if in.TaskID == "" && len(in.Budget) == 0 && len(in.Progress) == 0 {
		return InFlightTask{}
	}
	kept := cloneInFlight(in)
	l.flight[key] = kept
	return cloneInFlight(kept)
}

func cloneInFlight(in InFlightTask) InFlightTask {
	return InFlightTask{
		TaskID:   in.TaskID,
		Budget:   append([]byte(nil), in.Budget...),
		Progress: append([]byte(nil), in.Progress...),
	}
}

func idempotencyKey(in ActivateInput) string {
	return in.OwnerScope + "\x1f" + in.ProviderID + "\x1f" + in.ModelID + "\x1f" + in.IdempotencyKey
}
