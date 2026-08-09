package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/oklog/ulid/v2"
)

const maxElectronProviderFile = 1 << 20
const maxElectronProviders = 100

type MetadataMigrationStatus struct {
	State             string `json:"state"`
	Processed         int    `json:"processed"`
	Total             int    `json:"total"`
	Imported          int    `json:"imported"`
	Duplicates        int    `json:"duplicates"`
	Conflicts         int    `json:"conflicts"`
	ErrorCode         string `json:"errorCode,omitempty"`
	SourceFingerprint string `json:"sourceFingerprint,omitempty"`
}

type electronFile struct {
	Version   json.RawMessage    `json:"version"`
	Providers []electronProvider `json:"providers"`
}
type electronProvider struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Protocol        string          `json:"protocol"`
	BaseURL         string          `json:"baseUrl"`
	Models          []string        `json:"models"`
	Model           string          `json:"model"`
	DefaultModel    string          `json:"defaultModel"`
	CreatedAt       string          `json:"createdAt"`
	UpdatedAt       string          `json:"updatedAt"`
	EncryptedAPIKey json.RawMessage `json:"encryptedApiKey,omitempty"`
}
type inspectedElectronFile struct {
	raw                            []byte
	version, fingerprint, pathHash string
	providers                      []provider.Provider
	itemFP                         []string
	encrypted                      []string
	legacyProtocols                []string
	osCryptEncryptedKey            string
}

// ElectronCredentialCandidate is metadata plus the opaque safeStorage value
// from a pinned allowlisted source. It is only exposed to the Desktop Host
// visitor and must never be transported to Engine or Bridge.
type ElectronCredentialCandidate struct {
	SourceFingerprint   string
	ItemFingerprint     string
	ProviderID          string
	LegacyID            string
	SourceVersion       string
	Origin              string
	Protocol            provider.Protocol
	LegacyProtocol      string
	EncryptedBlob       string
	OSCryptEncryptedKey string
}

// InspectElectronProviderMetadata strictly validates a bounded legacy Electron file.
// It never decrypts or returns encryptedApiKey and never modifies the source.
func (s *Store) InspectElectronProviderMetadata(ctx context.Context, path string) (MetadataMigrationStatus, error) {
	in, err := inspectElectronFile(path)
	if err != nil {
		return MetadataMigrationStatus{}, err
	}
	status, found, err := s.metadataMigrationStatus(ctx, in.fingerprint)
	if err != nil {
		return status, err
	}
	if found {
		return status, nil
	}
	return MetadataMigrationStatus{State: "idle", Total: len(in.providers), SourceFingerprint: in.fingerprint}, nil
}

// RunElectronProviderMetadata imports metadata only. Credentials are deliberately
// discarded and every imported provider is marked requires_reentry.
func (s *Store) RunElectronProviderMetadata(ctx context.Context, path string) (MetadataMigrationStatus, error) {
	in, err := inspectElectronFile(path)
	if err != nil {
		return MetadataMigrationStatus{}, err
	}
	return s.runInspectedElectronProviderMetadata(ctx, in)
}

func (s *Store) runInspectedElectronProviderMetadata(ctx context.Context, in inspectedElectronFile) (MetadataMigrationStatus, error) {
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_metadata_migrations(source_fingerprint,source_path_hash,source_version,state,total,started_at,updated_at) VALUES(?,?,?,'running',?,?,?) ON CONFLICT(source_fingerprint) DO UPDATE SET state=CASE WHEN state='completed' THEN state ELSE 'running' END,error_code=NULL,updated_at=excluded.updated_at`, in.fingerprint, in.pathHash, in.version, len(in.providers), now, now)
	if err != nil {
		return MetadataMigrationStatus{}, err
	}
	for i, item := range in.providers {
		if err = s.importElectronProvider(ctx, in.fingerprint, in.itemFP[i], item, in.encrypted[i] != ""); err != nil {
			_, _ = s.db.ExecContext(context.Background(), `UPDATE provider_metadata_migrations SET state='failed',error_code='IMPORT_FAILED',updated_at=? WHERE source_fingerprint=?`, formatTime(time.Now().UTC()), in.fingerprint)
			status, statusErr := s.StatusElectronProviderMetadata(ctx, in.fingerprint)
			if statusErr != nil {
				return status, errors.Join(err, statusErr)
			}
			return status, err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE provider_metadata_migrations SET state='completed',error_code=NULL,updated_at=? WHERE source_fingerprint=?`, formatTime(time.Now().UTC()), in.fingerprint)
	if err != nil {
		return MetadataMigrationStatus{}, err
	}
	return s.StatusElectronProviderMetadata(ctx, in.fingerprint)
}

func (s *Store) StatusElectronProviderMetadata(ctx context.Context, fingerprint string) (MetadataMigrationStatus, error) {
	st, found, err := s.metadataMigrationStatus(ctx, fingerprint)
	if err != nil {
		return st, err
	}
	if !found {
		return MetadataMigrationStatus{State: "idle"}, nil
	}
	return st, nil
}

func (s *Store) metadataMigrationStatus(ctx context.Context, fingerprint string) (MetadataMigrationStatus, bool, error) {
	var st MetadataMigrationStatus
	var code sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT state,processed,total,imported,duplicates,conflicts,error_code,source_fingerprint FROM provider_metadata_migrations WHERE source_fingerprint=?`, fingerprint).Scan(&st.State, &st.Processed, &st.Total, &st.Imported, &st.Duplicates, &st.Conflicts, &code, &st.SourceFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return st, false, nil
	}
	if err != nil {
		return st, false, err
	}
	st.ErrorCode = code.String
	return st, true, nil
}

func (s *Store) importElectronProvider(ctx context.Context, sourceFP, itemFP string, item provider.Provider, hasCredential bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var already int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM provider_metadata_migration_items WHERE source_fingerprint=? AND item_fingerprint=?`, sourceFP, itemFP).Scan(&already); err != nil || already != 0 {
		if err == nil {
			return tx.Commit()
		}
		return err
	}
	result, detail, providerID := "imported", "metadata_imported", item.ID
	var existingID, name, protocolName, baseURL string
	err = tx.QueryRowContext(ctx, `SELECT id,name,protocol,base_url FROM providers WHERE legacy_id=?`, item.LegacyID).Scan(&existingID, &name, &protocolName, &baseURL)
	if err == nil {
		providerID = existingID
		if name == item.Name && protocolName == string(item.Protocol) && baseURL == item.BaseURL && modelsEqual(ctx, tx, existingID, item.Models) {
			result, detail = "duplicate", "legacy_id_identical"
		} else {
			result, detail = "conflict", "legacy_id_mismatch"
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	} else {
		fp, _ := provider.OriginFingerprint(item.Protocol, item.BaseURL)
		_, err = tx.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,origin_fingerprint) VALUES(?,?,?,?,?,NULL,'requires_reentry','enabled',?,?,1,?)`, item.ID, item.LegacyID, item.Name, item.Protocol, item.BaseURL, formatTime(item.CreatedAt), formatTime(item.UpdatedAt), fp)
		if err != nil {
			return err
		}
		if err = replaceModels(ctx, tx, item.ID, item.Models); err != nil {
			return err
		}
		metadata := fmt.Sprintf(`{"migration":"electron_provider_metadata","sourceFingerprint":"%s","credentialState":"requires_reentry"}`, sourceFP)
		_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`, deterministicULID("audit\x00"+sourceFP+"\x00"+itemFP), "provider.created", item.ID, "electron-metadata-migration", metadata, formatTime(time.Now().UTC()))
		if err != nil {
			return err
		}
	}
	credentialState := "none"
	if hasCredential && result != "conflict" {
		credentialState = "pending"
	} else if hasCredential {
		credentialState = "superseded"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO provider_metadata_migration_items(source_fingerprint,item_fingerprint,legacy_id,result,provider_id,detail_code,credential_migration_state) VALUES(?,?,?,?,?,?,?)`, sourceFP, itemFP, item.LegacyID, result, providerID, detail, credentialState)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE provider_metadata_migrations SET processed=processed+1,imported=imported+?,duplicates=duplicates+?,conflicts=conflicts+?,updated_at=? WHERE source_fingerprint=?`, boolInt(result == "imported"), boolInt(result == "duplicate"), boolInt(result == "conflict"), formatTime(time.Now().UTC()), sourceFP)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func inspectElectronFile(path string) (inspectedElectronFile, error) {
	var out inspectedElectronFile
	if path == "" || !filepath.IsAbs(path) || filepath.Base(path) != "providers.json" || strings.ContainsAny(path, "\x00\r\n") {
		return out, errors.New("unsafe Electron provider path")
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return out, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return out, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxElectronProviderFile {
		return out, errors.New("Electron provider file is not a bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxElectronProviderFile+1))
	if err != nil || len(raw) > maxElectronProviderFile {
		return out, errors.New("Electron provider file exceeds size limit")
	}
	return inspectElectronBytes(raw, filepath.Clean(path))
}

// inspectElectronBytes parses the bytes already read from a pinned source
// handle. sourceIdentity is used only for the privacy-preserving path hash.
func inspectElectronBytes(raw []byte, sourceIdentity string) (inspectedElectronFile, error) {
	var out inspectedElectronFile
	if len(raw) > maxElectronProviderFile {
		return out, errors.New("Electron provider file exceeds size limit")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc electronFile
	if err := dec.Decode(&doc); err != nil {
		return out, fmt.Errorf("invalid Electron provider JSON: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return out, errors.New("trailing Electron provider JSON")
	}
	version, err := electronVersion(doc.Version)
	if err != nil {
		return out, err
	}
	if len(doc.Providers) > maxElectronProviders {
		return out, errors.New("too many Electron providers")
	}
	out.raw, out.version = raw, version
	sum := sha256.Sum256(raw)
	out.fingerprint = hex.EncodeToString(sum[:])
	ps := sha256.Sum256([]byte(strings.ToLower(sourceIdentity)))
	out.pathHash = hex.EncodeToString(ps[:])
	seen := map[string]bool{}
	for _, p := range doc.Providers {
		item, err := normalizeElectronProvider(p)
		if err != nil {
			return out, err
		}
		if seen[item.LegacyID] {
			return out, errors.New("duplicate legacy provider id in source")
		}
		seen[item.LegacyID] = true
		canonical, _ := json.Marshal(p)
		fp := sha256.Sum256(canonical)
		out.itemFP = append(out.itemFP, hex.EncodeToString(fp[:]))
		out.providers = append(out.providers, item)
		out.legacyProtocols = append(out.legacyProtocols, p.Protocol)
		var encrypted string
		if len(p.EncryptedAPIKey) != 0 {
			_ = json.Unmarshal(p.EncryptedAPIKey, &encrypted)
		}
		out.encrypted = append(out.encrypted, encrypted)
	}
	return out, nil
}

func electronVersion(raw json.RawMessage) (string, error) {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return "", errors.New("missing Electron provider version")
	}
	switch x := v.(type) {
	case string:
		if x == "0.1" || x == "0.2" || x == "0.2.1" {
			return x, nil
		}
	case json.Number:
		if x.String() == "1" {
			return "0.1", nil
		}
		if x.String() == "2" {
			return "0.2", nil
		}
	}
	return "", errors.New("unsupported Electron provider version")
}
func normalizeElectronProvider(p electronProvider) (provider.Provider, error) {
	if len(p.ID) < 1 || len(p.ID) > 128 || len(p.Name) < 1 || len(p.Name) > 500 || len(p.CreatedAt) > 64 || len(p.UpdatedAt) > 64 {
		return provider.Provider{}, errors.New("invalid Electron provider fields")
	}
	if len(p.EncryptedAPIKey) > 0 {
		var ciphertext string
		if json.Unmarshal(p.EncryptedAPIKey, &ciphertext) != nil || len(ciphertext) > 65536 {
			return provider.Provider{}, errors.New("invalid Electron encrypted credential metadata")
		}
	}
	proto := provider.ProtocolOpenAICompatible
	if p.Protocol == "anthropic" {
		proto = provider.ProtocolAnthropic
	} else if p.Protocol != "openai" && p.Protocol != "openai_compatible" {
		return provider.Provider{}, errors.New("invalid Electron provider protocol")
	}
	base, err := provider.NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return provider.Provider{}, err
	}
	models := p.Models
	if len(models) == 0 && p.Model != "" {
		models = []string{p.Model}
	}
	if len(models) < 1 || len(models) > 50 {
		return provider.Provider{}, errors.New("invalid Electron provider models")
	}
	def := strings.TrimSpace(p.DefaultModel)
	if def == "" {
		def = strings.TrimSpace(models[0])
	}
	converted := make([]provider.Model, 0, len(models))
	seen := map[string]bool{}
	found := false
	for _, m := range models {
		m = strings.TrimSpace(m)
		if len(m) < 1 || len(m) > 200 || seen[m] {
			return provider.Provider{}, errors.New("invalid Electron provider model")
		}
		seen[m] = true
		is := m == def
		found = found || is
		converted = append(converted, provider.Model{ModelID: m, DisplayName: m, IsDefault: is})
	}
	if !found {
		return provider.Provider{}, errors.New("invalid Electron default model")
	}
	created, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if err != nil {
		return provider.Provider{}, errors.New("invalid Electron createdAt")
	}
	updated, err := time.Parse(time.RFC3339Nano, p.UpdatedAt)
	if err != nil {
		return provider.Provider{}, errors.New("invalid Electron updatedAt")
	}
	item := provider.Provider{ID: deterministicULID("provider\x00" + p.ID), LegacyID: p.ID, Name: p.Name, Protocol: proto, BaseURL: base, Models: converted, CredentialState: provider.CredentialRequiresReentry, Status: provider.StatusEnabled, CreatedAt: created.UTC(), UpdatedAt: updated.UTC(), Version: 1}
	return item, item.Validate()
}
func deterministicULID(s string) string {
	sum := sha256.Sum256([]byte(s))
	var id ulid.ULID
	copy(id[:], sum[:16])
	return id.String()
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func modelsEqual(ctx context.Context, q sqlRunner, id string, want []provider.Model) bool {
	got, err := listModelsWith(ctx, q, id)
	if err != nil || len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
