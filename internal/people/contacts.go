package people

import (
	"context"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/oklog/ulid/v2"
)

func (s *Service) StartDiscoveryIfEnabled() {
	s.RefreshPresence()
}

func (s *Service) RefreshPresence() {
	if s == nil || s.identity == nil {
		return
	}
	if s.identity.Public().DiscoveryEnabled {
		_ = s.startLAN()
		return
	}
	if s.lan != nil {
		s.lan.Stop()
	}
}

func (s *Service) CurrentBeacon() (Beacon, bool) {
	if s == nil || s.identity == nil {
		return Beacon{}, false
	}
	pub := s.identity.Public()
	if pub.Status == identity.StatusInvisible {
		return Beacon{}, false
	}
	return Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: pub.SubjectID, Nickname: pub.Nickname,
		Department: pub.Department, Title: pub.Title, OrgName: pub.OrgName, Status: string(pub.Status),
		PublicKey: pub.PublicKey, PairingHash: s.identity.PairingHash(), Port: s.advertisedPort(),
	}, true
}

func (s *Service) List(ctx context.Context) ([]Contact, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	items, err := s.store.ListContacts(ctx)
	if err != nil {
		return nil, err
	}
	self := s.identity.SubjectID()
	for i := range items {
		items[i].Self = items[i].SubjectID == self
		if items[i].TrustState != "discovered" {
			items[i].PairingHash = ""
		}
	}
	return items, nil
}

func (s *Service) Pair(ctx context.Context, in PairInput) (Contact, error) {
	if err := s.readyUnlocked(); err != nil {
		return Contact{}, err
	}
	code := strings.TrimSpace(in.PairingCode)
	if len(code) != 6 {
		return Contact{}, ErrPairing
	}
	self := s.identity.Public()
	now := nowRFC3339()
	c := Contact{
		SubjectID:  strings.TrimSpace(in.SubjectID),
		Nickname:   strings.TrimSpace(in.Nickname),
		PublicKey:  strings.ToLower(strings.TrimSpace(in.PublicKey)),
		Department: in.Department,
		Title:      in.Title,
		OrgName:    in.OrgName,
		TrustState: "trusted",
		Status:     "offline",
		LastSeenAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if c.SubjectID != "" {
		if existing, err := s.store.GetContact(ctx, c.SubjectID); err == nil {
			if existing.TrustState == "self" {
				return Contact{}, ErrInvalid
			}
			if existing.PairingHash != "" && identity.PairingHash(code, existing.SubjectID) != existing.PairingHash {
				return Contact{}, ErrPairing
			}
			if existing.TrustState == "discovered" && existing.PairingHash == "" {
				return Contact{}, ErrPairing
			}
			if existing.TrustState != "discovered" && identity.PairingHash(code, self.SubjectID) != s.identity.PairingHash() {
				return Contact{}, ErrPairing
			}
			existing.TrustState = "trusted"
			if c.Nickname != "" {
				existing.Nickname = c.Nickname
			}
			if c.PublicKey != "" {
				existing.PublicKey = c.PublicKey
			}
			existing.UpdatedAt = now
			if err := s.store.UpsertContact(ctx, existing); err != nil {
				return Contact{}, err
			}
			_ = s.ensureTCP()
			existing.PairingHash = ""
			return existing, nil
		}
	}
	if identity.PairingHash(code, self.SubjectID) != s.identity.PairingHash() {
		return Contact{}, ErrPairing
	}
	if c.SubjectID == "" {
		c.SubjectID = ulid.Make().String()
	}
	if c.Nickname == "" {
		c.Nickname = "同事"
	}
	if utf8.RuneCountInString(c.Nickname) > 64 {
		return Contact{}, ErrInvalid
	}
	if err := s.store.UpsertContact(ctx, c); err != nil {
		return Contact{}, err
	}
	c.PairingHash = ""
	return c, nil
}

func (s *Service) IngestBeacon(ctx context.Context, b Beacon, host string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if b.V != 1 || b.Kind != "lunitide-people" || b.SubjectID == "" || b.SubjectID == s.identity.SubjectID() {
		return nil
	}
	if b.Status == string(identity.StatusInvisible) {
		return nil
	}
	existing, err := s.store.GetContact(ctx, b.SubjectID)
	now := nowRFC3339()
	addr := joinHostPort(host, b.Port)
	if err == nil {
		if existing.TrustState == "self" {
			return nil
		}
		if existing.Blocked {
			return nil
		}
		existing.Nickname = nonempty(b.Nickname, existing.Nickname)
		existing.Department = b.Department
		existing.Title = b.Title
		existing.OrgName = b.OrgName
		if b.Status != "" {
			existing.Status = b.Status
		}
		if b.PublicKey != "" {
			existing.PublicKey = b.PublicKey
		}
		if b.PairingHash != "" {
			existing.PairingHash = b.PairingHash
		}
		if addr != "" {
			existing.HostAddr = addr
		}
		existing.LastSeenAt = now
		existing.UpdatedAt = now
		return s.store.UpsertContact(ctx, existing)
	}
	nick := strings.TrimSpace(b.Nickname)
	if nick == "" {
		nick = "同事"
	}
	return s.store.UpsertContact(ctx, Contact{
		SubjectID:   b.SubjectID,
		Nickname:    nick,
		Status:      nonempty(b.Status, "online"),
		Department:  b.Department,
		Title:       b.Title,
		OrgName:     b.OrgName,
		PublicKey:   b.PublicKey,
		PairingHash: b.PairingHash,
		TrustState:  "discovered",
		HostAddr:    addr,
		LastSeenAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

func (s *Service) DiscoveryGet() (enabled bool, pairingCode string) {
	if s == nil || s.identity == nil {
		return false, ""
	}
	pub := s.identity.Public()
	return pub.DiscoveryEnabled, pub.PairingCode
}

func (s *Service) DiscoverySet(ctx context.Context, enabled bool) (identity.Public, error) {
	if err := s.readyUnlocked(); err != nil {
		return identity.Public{}, err
	}
	pub, err := s.identity.SetDiscovery(ctx, enabled)
	if err != nil {
		return identity.Public{}, err
	}
	if enabled {
		if err := s.startLAN(); err != nil {
			_, _ = s.identity.SetDiscovery(ctx, false)
			return identity.Public{}, err
		}
	} else if s.lan != nil {
		s.lan.Stop()
	}
	return pub, nil
}

func (s *Service) startLAN() error {
	_ = s.ensureTCP()
	if s.lan == nil {
		s.lan = NewLAN()
	}
	return s.lan.Start(func() Beacon {
		b, ok := s.CurrentBeacon()
		if !ok {
			return Beacon{}
		}
		return b
	}, func(b Beacon, host string) {
		_ = s.IngestBeacon(context.Background(), b, host)
	})
}

func nonempty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func (s *Service) AddPeer(ctx context.Context, hostAddr string) (Contact, error) {
	if err := s.readyUnlocked(); err != nil {
		return Contact{}, err
	}
	addr, err := parsePeerAddr(hostAddr)
	if err != nil {
		return Contact{}, err
	}
	if err := s.ensureTCP(); err != nil {
		return Contact{}, err
	}
	hello, err := s.dialHello(addr)
	if err != nil {
		return Contact{}, err
	}
	host, _, _ := net.SplitHostPort(addr)
	if err := s.IngestBeacon(ctx, hello.beacon(), host); err != nil {
		return Contact{}, err
	}
	if existing, getErr := s.store.GetContact(ctx, hello.SubjectID); getErr == nil {
		if existing.Blocked {
			return Contact{}, ErrBlocked
		}
		existing.HostAddr = addr
		existing.UpdatedAt = nowRFC3339()
		_ = s.store.UpsertContact(ctx, existing)
		existing.Self = existing.SubjectID == s.identity.SubjectID()
		existing.PairingHash = ""
		return existing, nil
	}
	return s.store.GetContact(ctx, hello.SubjectID)
}

func (s *Service) UpdateContact(ctx context.Context, subjectID string, patch ContactPatch) (Contact, error) {
	if err := s.readyUnlocked(); err != nil {
		return Contact{}, err
	}
	c, err := s.store.GetContact(ctx, subjectID)
	if err != nil {
		return Contact{}, ErrNotFound
	}
	if c.TrustState == "self" && patch.Blocked != nil && *patch.Blocked {
		return Contact{}, ErrInvalid
	}
	if patch.Remark != nil {
		remark := strings.TrimSpace(*patch.Remark)
		if utf8.RuneCountInString(remark) > 64 {
			return Contact{}, ErrInvalid
		}
		c.Remark = remark
	}
	if patch.Blocked != nil {
		c.Blocked = *patch.Blocked
	}
	c.UpdatedAt = nowRFC3339()
	if err := s.store.UpsertContact(ctx, c); err != nil {
		return Contact{}, err
	}
	c.Self = c.SubjectID == s.identity.SubjectID()
	c.PairingHash = ""
	return c, nil
}

func (s *Service) advertisedPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tcpPort > 0 {
		return s.tcpPort
	}
	return defaultTCP
}

func joinHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if port <= 0 {
		port = defaultTCP
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func parsePeerAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") {
		return "", ErrInvalid
	}
	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw, nil
	}
	if ip := net.ParseIP(raw); ip != nil {
		return net.JoinHostPort(raw, strconv.Itoa(defaultTCP)), nil
	}
	return "", ErrInvalid
}
