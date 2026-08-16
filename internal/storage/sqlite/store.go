package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
	modernsqlite "modernc.org/sqlite"
)

// SecureRoot is the capability required to open production storage. The root
// implementation pins the directory and verifies each database sidecar.
type SecureRoot interface {
	FilePath(name string) (string, error)
	ProtectRegularFile(name string) error
}

type Store struct {
	db        *sql.DB
	path      string
	root      SecureRoot
	names     []string
	idEntropy io.Reader
}

// OpenSecure is the only production open API. A caller cannot supply a DSN.
func OpenSecure(ctx context.Context, root SecureRoot, name string) (*Store, error) {
	if filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".db") {
		return nil, fmt.Errorf("unsafe database filename %q", name)
	}
	path, err := root.FilePath(name)
	if err != nil {
		return nil, err
	}
	names := []string{name, name + "-wal", name + "-shm", name + "-journal"}
	for _, n := range names {
		if err := root.ProtectRegularFile(n); err != nil {
			return nil, err
		}
	}
	return open(ctx, path, root, names)
}

// Open is retained for isolated non-Windows tests; production must use OpenSecure.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Ext(path) != ".db" || strings.ContainsAny(path, "\x00\r\n") {
		return nil, fmt.Errorf("unsafe SQLite path %q", path)
	}
	return open(ctx, filepath.Clean(path), nil, nil)
}

func open(ctx context.Context, path string, root SecureRoot, names []string) (*Store, error) {
	// The driver receives a filename, not a URI/DSN; special characters remain data.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: path, root: root, names: names, idEntropy: rand.Reader}
	if err := s.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if root != nil {
		for _, n := range names {
			if err := root.ProtectRegularFile(n); err != nil {
				db.Close()
				return nil, err
			}
		}
	}
	return s, nil
}

func (s *Store) Close() error {
	err := s.db.Close()
	if s.root != nil {
		for _, n := range s.names {
			if e := s.root.ProtectRegularFile(n); err == nil && e != nil {
				err = e
			}
		}
	}
	return err
}

// ContextReader returns a contextapp.Reader backed by this store, enabling
// the chat engine to assemble durable session context for model input.
func (s *Store) ContextReader() contextapp.Reader {
	return newContextAdapter(s)
}

func (s *Store) ResolveProvider(ctx context.Context, id string) (provider.Provider, error) {
	return s.Get(ctx, id)
}
func (s *Store) IsCredentialReferenceAdopted(ctx context.Context, ref secret.Ref) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM credential_adoptions a JOIN providers p ON p.id=a.provider_id WHERE a.credential_ref=? AND a.provider_id=? AND a.origin=? AND a.protocol=? AND p.credential_ref=a.credential_ref AND p.deleted_at IS NULL`, ref.CredentialRef, ref.ProviderID, ref.Origin, ref.Protocol).Scan(&n)
	return n == 1, err
}

func (s *Store) ResolveCredentialBinding(ctx context.Context, id string) (secret.Ref, bool, error) {
	var ref secret.Ref
	var baseURL string
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(credential_ref,''),base_url,protocol FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&ref.ProviderID, &ref.CredentialRef, &baseURL, &ref.Protocol)
	if err == sql.ErrNoRows {
		return ref, false, provider.ErrNotFound
	}
	if err != nil {
		return ref, false, err
	}
	if ref.CredentialRef == "" {
		return ref, false, nil
	}
	ref.Origin, err = provider.NormalizeOrigin(baseURL)
	return ref, true, err
}

var manifest = []struct{ name, checksum string }{
	{"0001_provider.sql", "d66080078c26cb33fe9761d6abe0e04c32cfb5a2e18bd921801a3fb1380adc94"},
	{"0002_provider_production.sql", "42934d53c6c27cdef40bf3a58fce16b1d2025d6a547e43d985457b702ea8f5cd"},
	{"0003_provider_app.sql", "bf7ed1d958fcc04e180a9b888edb1b0f0e51cd0071227f80fa588d737d622835"},
	{"0004_model_sync_claims.sql", "160970b0aac29327774957e19acebdbb1b2f463a3c742c772e4809c29096ffff"},
	{"0005_electron_provider_metadata.sql", "8b9ab1a5b7600555a7674113fa2b1cee106e16274629b4957d4d92234439fb1f"},
	{"0006_electron_credential_adoption.sql", "0417131a4abe5e9d2c5f70809c542a0bdde36385dc710ef031bf84c39ad0a936"},
	{"0007_project.sql", "eb31143f8347a75a3abee8670fd8c9a047fe712db64c4e8378720d08698864a3"},
	{"0008_session.sql", "08b1478bd48900da0f89a83de15c7517fbd6445790759d2374c449f67d436620"},
	{"0009_message.sql", "5695394548899d026a045f8cdbcb0ed869920563ba88c9f0b2766f85e4d06220"},
	{"0010_token_ledger.sql", "5149c8b0db94cbc87a1451351c47bc132fac75c1b420a45faef4e707c6dcbabc"},
	{"0011_compaction_checkpoint.sql", "e3cae92156656cedb1cf336b3987e046f60eafec68e3a325d7253d9c5f2a6061"},
	{"0012_planning.sql", "4356919714fb5b637ac011db8ef2899b2711282b8ce0b32c5621af11ea5ea7bf"},
	{"0013_governance.sql", "9def982ac0e9aa359d31e387c1a87000bdc365a8854f094f11757cb53bd685d0"},
	{"0014_memory.sql", "9c568f2422970421a9a30b34a096510af622b74961c3cad583a4cee206f03796"},
	{"0015_ontology.sql", "9bbd48db722e2e896367cb0c3e658c58d4564d213aadfa6955d7499dc0d7cdd9"},
	{"0016_skill.sql", "3c1b38188bc150f201a94daa5993626738154af2e6811edd66ccd56121455e58"},
	{"0017_stage.sql", "d2112f8276a176f84eab1c10bcb51cbbd3099fe1f5b4238873b1059b7af0d8ed"},
	{"0018_extended_entities.sql", "dc9b65b15dbb554f3750fcf721a37abd6540d7601ab4b0606b75ef3feb70a71d"},
	{"0019_durable_chat.sql", "4dd945a0e2c44c80a92a76079a0d0148470884f9fe036d02d66448d864d32a5a"},
	{"0020_model_context_window.sql", "ee48181f46441bba742086251bc11848a968f7c005d5a14f35e8b1dc3216c0ed"},
	{"0021_token_ledger_identity.sql", "74af0a4ee888a12173173341b0b5e4179a08aeb432f7e4f80ec322b24fb68ffc"},
	{"0022_message_tool_role.sql", "d7969dfc2afc8b97cbbec333c9414bb09ad6283fe46bcb738d27a22f8bf2de6e"},
	{"0023_handoff_imports.sql", "92b5d8c4002c921ae7d5568a0c5f00a4b4f17c2b9091c16a5bca7ff4d9c6821a"},
	{"0024_attachments.sql", "8a15e28ec91c2e754be3047b83714e089106be6160c4cf75f8e12b4643ee3f92"},
	{"0025_compaction_activation.sql", "ac97bed5f44b258013ff253db551167ba73289c793f990ec11d3db03fe056e80"},
	{"0026_token_ledger_revision_identity.sql", "e11a5238e49c32e0d6ef6f9f80f29b9fd0c5d21bee499926e67570e174a85138"},
	{"0027_token_ledger_remove_legacy_unique.sql", "6a570af69cc5c0bb4624bf7926036fbe3149d07090c409ce82bed23e9360bf2e"},
	{"0028_agent_orchestration.sql", "6bae2d89d3185193370ac9b6d43dbdc675d84e9b5106416dfacb362b413a2f2e"},
	{"0029_session_update.sql", "dd1fc0179d2621f5f5bb12a8d3fee9331e63fa33e2fea3fbd7ef7b99fee895a7"},
	{"0030_m4_agent_runtime.sql", "53b4d91e580ca37acb51195a07cc2d9d66cdf5110b11c552b03a629b51711c11"},
	{"0031_m4_review_workspace.sql", "eab8fb463fbb07e44298340b24d9133c0d27b1b68ef76a2f301cc9d34fa8ae7f"},
	{"0032_m4_changeset.sql", "abdc06387435685606f2d4605b20e8280d77d4c2440da9d0bfb4415df3a06886"},
	{"0033_m4_command.sql", "b9c8bbb4f1517e19d73ae05c4ed36f8590630ac0735ee1767f127aceac296d2d"},
	{"0034_m4_web.sql", "f568b98103936fa53771c7d14e1701f98df19594089f67a4a14b9e98220e6520"},
	{"0035_m4_plan_evidence.sql", "88f77afd792c405715d3a3f475fdfedd0e2f615e2fc7568bfe62ad168236b0db"},
	{"0036_m4_durable_run_core.sql", "6f3862adb0ba2169d5273b5137264d177b264c2e7b1b63b38696ac8fdb93e020"},
	{"0037_m4_p0_review_effects.sql", "01063f95414706039e2d3bd8b113b2bb27af07d13f1687b5fcc216ae50d78993"},
	{"0038_m4_changeset_effect_receipt.sql", "fd3ec3ca2263e6c0531cc3d86b3c7737af800ccd1ee789205e732e582c188079"},
	{"0039_m4_command_durable_review.sql", "d39cd330bf51ebc6ac8e24961a23632b8a1e1f10d2bed9f21134b130b71bb67a"},
	{"0040_message_rewind.sql", "810e950ca527a936e4c3be390fa950de69b99312473e774ea375ba9e034699e1"},
	{"0041_m5_scope_seal.sql", "ead9e044981a8dfd5950376827a348aea8319f35d8b7e1eb08574543a44cb768"},
	{"0042_m5_runtime.sql", "e6d7168e36e2efe8f6d4c3f9fa358af45df1e7a26afe393ae8fb2c5c6b38c97e"},
	{"0043_m5_changeset_rollback.sql", "089042d7063e4a73ffee92ce0f7f703cdd6a5cece06600879bd0e0444ab54d23"},
	{"0044_m6_extension_supply_chain.sql", "e18fd15c7d72014d5b8d6cee758ec1a9f75618fab8ebfd2ebc3958c1d501924a"},
	{"0045_m6_connector_catalog.sql", "5ef2f7789ba27d1aa63bd5aeeb2a07611d0a35837cdda1e5f444ef5fb0837aa6"},
	{"0046_m6_cloud_delegation.sql", "e73af74900e7aac397025f467319555d90f3154fd51e3485a7218385d2463fb7"},
	{"0047_m6_audit_actions.sql", "78d0e5c1316bf48e9449a1ab311d5f88b30857b5c00e0c72e7a05e50beaaba63"},
	{"0048_m6_audit_delegation.sql", "696dd47d07937cd35d23f5d52a5908fe9307946e9905fa76a00e929ec8624a72"},
	{"0049_m6_audit_merge.sql", "07acd8206b3164004d4e2dc839cb54e044a8305749b3a531ff446f6b2c144424"},
	{"0050_m6_audit_stdio_worker.sql", "031b9092944af6a7fd2a9dc7efba7aba9d75d5f34a7208ed5d67ee1dada333b7"},
	{"0051_m7_workflow_backbone.sql", "1c0161b061d7c73e2e7dff2eff2091d3e988b037bc46f1c687d3f362a95de88b"},
	{"0052_m7_evidence_trace.sql", "e37f1281c9d8c99fc45c49e4b84218e576a71cccc44e0758dee816733861a0f9"},
	{"0053_m6_s5c_skill_routing_cloud.sql", "352ef999925e4e48fd2aa157398fa8ecaada8128dff32bae159ea330b2d73f82"},
	{"0054_audit_actions_m0m3.sql", "d5241af7c46855756020ff348d9e9652e68b751482255ebdb6d71e8ad2f041e8"},
	{"0055_audit_actions_policy_skill.sql", "102cd375292643096d4386922f66c33d34f246e294c44f787c3a5431fbdf2e30"},
	{"0056_m7_release_promotion.sql", "d76bdd77ce29472aaa19449ce33822bdedc705b545b8383808455148afc5986a"},
	{"0057_m7_appupdate_split.sql", "453eb3ade27812b898b5ea5a871524cd7455b71cd9f38fdfe9a50313ebab8e45"},
	{"0058_m7_subagent.sql", "d3e20ff00b7dee3d96390e52be59ef7333c5d311271197001c4d5fb4d37ba843"},
	{"0059_m7_toolgap.sql", "c2df6bbb1891ef71732a746321894187b3ffe3591bd453a5ea964c781d891543"},
	{"0060_m7_mcp.sql", "ea91d17c84e9665917eeb0288cb7e7a2a03df409bdcea601b0b41f4becdf4b37"},
	{"0061_m8_memory_core.sql", "92c567ec9278150da073f76c961706e9e7875f6872ed3be0bc670bfdc510fc2c"},
	{"0062_m8_knowledge_graph.sql", "c7079ee26585dc0f4e3ec1df731de949c77feb424d507cf67f642979a967daff"},
	{"0063_m8_handoff_tombstone.sql", "2ec7e61a9dd5a49d915cbbba1e50c90c672636969abe4d7af914cf206163ff65"},
	{"0064_m8_automation.sql", "abe84510420507c2782af1e76c01afe149f96638a45129b80ec37d0ea0a4ecab"},
	{"0065_m8_learning_candidates.sql", "192454e89534b8136c702212fedcb2892b538f9652edc66121c7eeb5c8af5008"},
	{"0066_m8_collab_gate.sql", "257d83d075238b8f233616160f210bc8b19a7b17131d5035e50e55f1659c1498"},
	{"0067_m8_plugin.sql", "e76911e6aebcabddb34f4eaa7c5ff554cf89ea61292f07f74717d3cc35a201d0"},
	{"0068_m8_expert.sql", "4f0a2ea9e3d86f3c7cb465c0fa8fbfcf41ff519f897ada6f247c163a07132133"},
	{"0069_m9_org_foundation.sql", "2c3b93f0a47a944c506cd56241ce69a142acad87434e0d691d2327119e01cde1"},
	{"0070_m7_project_lifecycle.sql", "6d770e3c76938832fa1056ee75a424db0f5b0a1e224ba5790d934953bf1d4c59"},
	{"0071_m10_memory_nomination.sql", "ef1f79ac1ad53933a4728a526f46486d01293d948b9d3acb9e223d82b1c1ffb6"},
}

const releasedV1ManifestTypo = "ede2beec8f6d9f70edd2490688a5fd8b4e6631ddd2321f689b42abb12883d02d"

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) initialize(ctx context.Context) (resultErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// foreign_keys is OFF during migration to allow table rebuilds that modify
	// CHECK constraints on tables referenced by foreign keys. It is re-enabled
	// and integrity-checked after migration completes.
	defer func() {
		if resultErr != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
		_, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	}()
	for _, p := range []struct {
		set, query string
		want       any
	}{
		{"PRAGMA foreign_keys=OFF", "PRAGMA foreign_keys", int64(0)},
		{"PRAGMA busy_timeout=5000", "PRAGMA busy_timeout", int64(5000)},
		{"PRAGMA trusted_schema=OFF", "PRAGMA trusted_schema", int64(0)},
	} {
		if _, err := conn.ExecContext(ctx, p.set); err != nil {
			return fmt.Errorf("set %s: %w", p.set, err)
		}
		var got int64
		if err := conn.QueryRowContext(ctx, p.query).Scan(&got); err != nil || got != p.want {
			return fmt.Errorf("verify %s: got %d: %w", p.query, got, err)
		}
	}
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("enable WAL: %q: %w", mode, err)
	}
	// Serializes journal validation, exact legacy fingerprint validation, and backfill.
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("begin exclusive migration validation: %w", err)
	}
	if err := ensureMigrationJournal(ctx, conn); err != nil {
		return err
	}
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	if err := validateJournal(ctx, conn, files); err != nil {
		return err
	}
	legacy := []int{}
	corrections := []int{}
	for i, m := range manifest {
		var checksum sql.NullString
		err := conn.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=?`, m.name).Scan(&checksum)
		if err == sql.ErrNoRows {
			if i == 1 {
				if err = validateV1Schema(ctx, conn); err != nil {
					return err
				}
				if err = s.preflightV1(ctx, conn); err != nil {
					return err
				}
			}
			body, _ := migrations.Files.ReadFile(m.name)
			if _, err = conn.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("apply migration %s: %w", m.name, err)
			}
			if i == 1 {
				if err = s.migrateV1Data(ctx, conn); err != nil {
					return err
				}
			}
			if i == 2 {
				if err = backfillOriginFingerprints(ctx, conn); err != nil {
					return err
				}
			}
			if _, err = conn.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at,checksum) VALUES(?,?,?)`, m.name, time.Now().UTC().Format(time.RFC3339Nano), m.checksum); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !checksum.Valid {
			legacy = append(legacy, i)
		} else if i == 0 && checksum.String == releasedV1ManifestTypo {
			// The first native release candidate applied the current embedded V1
			// bytes but journaled a stale manifest hash. Defer correction until the
			// complete schema fingerprint and data invariants pass below.
			corrections = append(corrections, i)
		} else if checksum.String != m.checksum {
			return fmt.Errorf("migration %s checksum mismatch", m.name)
		}
	}
	versionBefore, fingerprint, err := validateSchema(ctx, conn)
	if err != nil {
		return err
	}
	if err = validateDataInvariants(ctx, conn); err != nil {
		return err
	}
	for _, i := range legacy {
		r, err := conn.ExecContext(ctx, `UPDATE schema_migrations SET checksum=? WHERE version=? AND checksum IS NULL`, manifest[i].checksum, manifest[i].name)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return fmt.Errorf("legacy journal changed concurrently")
		}
	}
	for _, i := range corrections {
		r, err := conn.ExecContext(ctx, `UPDATE schema_migrations SET checksum=? WHERE version=? AND checksum=?`, manifest[i].checksum, manifest[i].name, releasedV1ManifestTypo)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return fmt.Errorf("migration checksum correction changed concurrently")
		}
	}
	versionAfter, fingerprintAfter, err := validateSchema(ctx, conn)
	if err != nil {
		return err
	}
	if versionBefore != versionAfter || fingerprint != fingerprintAfter {
		return fmt.Errorf("schema changed during legacy checksum backfill")
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	// Verify foreign key integrity after migration, then re-enable enforcement.
	fkRows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("foreign key integrity check: %w", err)
	}
	if fkRows.Next() {
		fkRows.Close()
		return fmt.Errorf("foreign key integrity check failed after migration")
	}
	fkRows.Close()
	return nil
}

func validateV1Schema(ctx context.Context, q sqlRunner) error {
	want := map[string]string{
		"providers":       "CREATE TABLE providers (\n    id TEXT PRIMARY KEY,\n    legacy_id TEXT UNIQUE,\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),\n    credential_ref TEXT,\n    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),\n    deleted_at TEXT\n)",
		"provider_models": "CREATE TABLE provider_models (\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 500),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 500),\n    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),\n    position INTEGER NOT NULL DEFAULT 0,\n    PRIMARY KEY (provider_id, model_id)\n)",
	}
	for name, expected := range want {
		var got string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type='table' AND name=?`, name).Scan(&got); err != nil || got != expected {
			return fmt.Errorf("schema definition mismatch for table:%s", name)
		}
	}
	return nil
}

type legacyProvider struct {
	id, legacyID, name, protocol, baseURL, credentialRef, credentialState, created, updated string
	version                                                                                 int64
	deleted                                                                                 sql.NullString
	newID                                                                                   string
}

func (s *Store) preflightV1(ctx context.Context, q sqlRunner) error {
	rows, err := q.QueryContext(ctx, `SELECT id,COALESCE(legacy_id,''),name,protocol,base_url,COALESCE(credential_ref,''),credential_state,created_at,updated_at,version,deleted_at FROM providers ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p legacyProvider
		if err := rows.Scan(&p.id, &p.legacyID, &p.name, &p.protocol, &p.baseURL, &p.credentialRef, &p.credentialState, &p.created, &p.updated, &p.version, &p.deleted); err != nil {
			return err
		}
		if len(p.name) > 500 || len(p.baseURL) > 2048 || len(p.credentialRef) > 500 {
			return fmt.Errorf("provider migration required: provider %q has an overlong field", p.id)
		}
		if _, err := canonicalOrLegacyID(p.id, p.legacyID); err != nil {
			return err
		}
		var count, defaults, overlong int
		if err := q.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(is_default),0),COALESCE(sum(length(model_id)>200 OR length(display_name)>200),0) FROM provider_models WHERE provider_id=?`, p.id).Scan(&count, &defaults, &overlong); err != nil {
			return err
		}
		if count < 1 || count > 50 {
			return fmt.Errorf("provider migration required: provider %q must have 1-50 models (has %d)", p.id, count)
		}
		if defaults != 1 {
			return fmt.Errorf("provider migration required: provider %q must have exactly one default model (has %d); a default cannot be guessed", p.id, defaults)
		}
		if overlong != 0 {
			return fmt.Errorf("provider migration required: provider %q has overlong model fields", p.id)
		}
	}
	return rows.Err()
}

// Strict policy: retain only uppercase canonical ULIDs. Other IDs are copied
// verbatim to legacy_id and replaced; conflicting pre-existing legacy_id fails.
func canonicalOrLegacyID(id, legacyID string) (bool, error) {
	parsed, err := ulid.ParseStrict(id)
	if err == nil && parsed.String() == id {
		return true, nil
	}
	if legacyID != "" && legacyID != id {
		return false, fmt.Errorf("provider migration required: legacy provider %q already has different legacy_id %q", id, legacyID)
	}
	return false, nil
}

func (s *Store) newULID(now time.Time) (string, error) {
	id, err := ulid.New(ulid.Timestamp(now.UTC()), s.idEntropy)
	if err != nil {
		return "", fmt.Errorf("generate provider ULID: %w", err)
	}
	return id.String(), nil
}

func (s *Store) migrateV1Data(ctx context.Context, q sqlRunner) error {
	rows, err := q.QueryContext(ctx, `SELECT id,COALESCE(legacy_id,''),name,protocol,base_url,COALESCE(credential_ref,''),credential_state,created_at,updated_at,version,deleted_at FROM providers_v1 ORDER BY id`)
	if err != nil {
		return err
	}
	var items []legacyProvider
	for rows.Next() {
		var p legacyProvider
		if err = rows.Scan(&p.id, &p.legacyID, &p.name, &p.protocol, &p.baseURL, &p.credentialRef, &p.credentialState, &p.created, &p.updated, &p.version, &p.deleted); err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	used := map[string]bool{}
	for i := range items {
		keep, _ := canonicalOrLegacyID(items[i].id, items[i].legacyID)
		if keep {
			items[i].newID = items[i].id
			used[items[i].id] = true
		}
	}
	for i := range items {
		p := &items[i]
		if p.newID == "" {
			for attempts := 0; attempts < 16; attempts++ {
				p.newID, err = s.newULID(time.Now())
				if err != nil {
					return err
				}
				if !used[p.newID] {
					break
				}
			}
			if used[p.newID] {
				return fmt.Errorf("generate unique provider ULID: retry limit reached")
			}
			used[p.newID] = true
			p.legacyID = p.id
		}
		ref, state := p.credentialRef, p.credentialState
		if ref == "" && state == "configured" {
			state = "missing"
		}
		if ref != "" && state != "configured" {
			ref = ""
			state = "requires_reentry"
		}
		if _, err = q.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,deleted_at) VALUES(?,?,?,?,?,NULLIF(?,''),?,'enabled',?,?,?,?)`, p.newID, nullString(p.legacyID), p.name, p.protocol, p.baseURL, ref, state, p.created, p.updated, p.version, p.deleted); err != nil {
			return fmt.Errorf("migrate provider %q: %w", p.id, err)
		}
		mr, e := q.QueryContext(ctx, `SELECT model_id,display_name,is_default FROM provider_models_v1 WHERE provider_id=? ORDER BY position,rowid,model_id`, p.id)
		if e != nil {
			return e
		}
		pos := 0
		for mr.Next() {
			var mid, dn string
			var def bool
			if e = mr.Scan(&mid, &dn, &def); e != nil {
				mr.Close()
				return e
			}
			if _, e = q.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position) VALUES(?,?,?,?,?)`, p.newID, mid, dn, def, pos); e != nil {
				mr.Close()
				return e
			}
			pos++
		}
		if e = mr.Close(); e != nil {
			return e
		}
	}
	if _, err = q.ExecContext(ctx, `DROP TABLE provider_models_v1`); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `DROP TABLE providers_v1`)
	return err
}

func validateDataInvariants(ctx context.Context, q sqlRunner) error {
	var bad int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM providers p WHERE (SELECT count(*) FROM provider_models m WHERE m.provider_id=p.id) NOT BETWEEN 1 AND 50 OR (SELECT count(*) FROM provider_models m WHERE m.provider_id=p.id AND m.is_default=1)<>1 OR (credential_ref IS NOT NULL)<>(credential_state='configured')`).Scan(&bad)
	if err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("provider data invariant violation: %d invalid provider graphs", bad)
	}
	rows, err := q.QueryContext(ctx, `SELECT id,name,project_code,project_type,description,summary,objective,client,contract_no,amount,budget,plan_start,plan_end,remark,close_reason,status,created_at,updated_at,version FROM projects`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var p project.Project
		var created, updated string
		if err = rows.Scan(&p.ID, &p.Name, &p.ProjectCode, &p.Type, &p.Description, &p.Summary, &p.Objective, &p.Client, &p.ContractNo, &p.Amount, &p.Budget, &p.PlanStart, &p.PlanEnd, &p.Remark, &p.CloseReason, &p.Status, &created, &updated, &p.Version); err != nil {
			return err
		}
		if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return fmt.Errorf("project data invariant violation: invalid created_at: %w", err)
		}
		if p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return fmt.Errorf("project data invariant violation: invalid updated_at: %w", err)
		}
		if err = p.Validate(); err != nil {
			return fmt.Errorf("project data invariant violation: %w", err)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if count > 100 {
		return fmt.Errorf("project data invariant violation: capacity %d exceeds 100", count)
	}
	rows, err = q.QueryContext(ctx, `SELECT id,project_id,title,status,created_at,updated_at,version FROM sessions`)
	if err != nil {
		return err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var v session.Session
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Status, &created, &updated, &v.Version); err != nil {
			return err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return fmt.Errorf("session data invariant violation: %w", err)
		}
		v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return fmt.Errorf("session data invariant violation: %w", err)
		}
		if err = v.Validate(); err != nil {
			return fmt.Errorf("session data invariant violation: %w", err)
		}
		counts[v.ProjectID]++
		if counts[v.ProjectID] > 100 {
			return fmt.Errorf("session data invariant violation: project capacity exceeds 100")
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("session data invariant validation failed: %w", err)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT id,project_id,phase,title,status,created_at,updated_at,version FROM stages`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v stage.Stage
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Phase, &v.Title, &v.Status, &created, &updated, &v.Version); err != nil {
			rows.Close()
			return err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			rows.Close()
			return fmt.Errorf("stage data invariant violation: %w", err)
		}
		v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			rows.Close()
			return fmt.Errorf("stage data invariant violation: %w", err)
		}
		if err = v.Validate(); err != nil {
			rows.Close()
			return fmt.Errorf("stage data invariant violation: %w", err)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("stage data invariant validation failed: %w", err)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT m.id,m.session_id,m.role,m.status,m.sequence,MAX(CASE WHEN p.ordinal=1 AND p.type='text' THEN p.text END),m.created_at,count(p.message_id),count(CASE WHEN p.ordinal=1 AND p.type='text' THEN 1 END) FROM messages m LEFT JOIN message_parts p ON p.message_id=m.id GROUP BY m.id ORDER BY m.session_id,m.sequence`)
	if err != nil {
		return err
	}
	messageCounts := map[string]int64{}
	for rows.Next() {
		var v message.Message
		var created string
		var text sql.NullString
		var parts, validParts int
		if err = rows.Scan(&v.ID, &v.SessionID, &v.Role, &v.Status, &v.Sequence, &text, &created, &parts, &validParts); err != nil {
			rows.Close()
			return err
		}
		if parts != 1 || validParts != 1 || !text.Valid {
			rows.Close()
			return fmt.Errorf("message data invariant violation")
		}
		v.Text = text.String
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		messageCounts[v.SessionID]++
		if err != nil || v.Validate() != nil || messageCounts[v.SessionID] != v.Sequence {
			rows.Close()
			return fmt.Errorf("message data invariant violation")
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	var badState int
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM message_session_state st LEFT JOIN sessions s ON s.id=st.session_id WHERE s.id IS NULL OR st.text_bytes>? OR st.last_sequence<>(SELECT COALESCE(max(sequence),0) FROM messages WHERE session_id=st.session_id) OR st.message_count<>(SELECT count(*) FROM messages WHERE session_id=st.session_id) OR st.text_bytes<>(SELECT COALESCE(sum(length(CAST(p.text AS BLOB))),0) FROM messages m JOIN message_parts p ON p.message_id=m.id WHERE m.session_id=st.session_id)`, message.WorkspaceTextQuotaBytes).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message state invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM sessions)<>(SELECT count(*) FROM message_session_state)`).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message state invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM message_project_usage u LEFT JOIN projects p ON p.id=u.project_id WHERE p.id IS NULL OR u.text_bytes>? OR u.text_bytes<>(SELECT COALESCE(sum(length(CAST(mp.text AS BLOB))),0) FROM sessions s JOIN messages m ON m.session_id=s.id JOIN message_parts mp ON mp.message_id=m.id WHERE s.project_id=u.project_id)`, message.ProjectTextQuotaBytes).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message project usage invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM projects)<>(SELECT count(*) FROM message_project_usage)`).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message project usage invariant violation: %w", err)
	}
	var workspaceActual, workspaceStored int64
	if err = q.QueryRowContext(ctx, `SELECT COALESCE(sum(length(CAST(text AS BLOB))),0) FROM message_parts`).Scan(&workspaceActual); err != nil {
		return err
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(CASE WHEN singleton=1 THEN text_bytes END),-1) FROM message_workspace_usage`).Scan(&badState, &workspaceStored); err != nil || badState != 1 || workspaceStored != workspaceActual || workspaceStored > message.WorkspaceTextQuotaBytes {
		return fmt.Errorf("message workspace usage invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM messages m WHERE (SELECT count(*) FROM message_parts p WHERE p.message_id=m.id)<>1`).Scan(&bad); err != nil || bad != 0 {
		return fmt.Errorf("message data invariant violation: %w", err)
	}
	rows, err = q.QueryContext(ctx, `SELECT response_json FROM idempotency_records WHERE operation='message.append'`)
	if err != nil {
		return err
	}
	var replayResponses [][]byte
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return fmt.Errorf("message idempotency data invariant violation")
		}
		replayResponses = append(replayResponses, append([]byte(nil), raw...))
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, raw := range replayResponses {
		var replay message.Message
		if json.Unmarshal(raw, &replay) != nil || replay.Validate() != nil {
			return fmt.Errorf("message idempotency data invariant violation")
		}
		var authoritative message.Message
		var created string
		if err = q.QueryRowContext(ctx, `SELECT m.id,m.session_id,m.role,m.status,m.sequence,p.text,m.created_at FROM messages m JOIN message_parts p ON p.message_id=m.id AND p.ordinal=1 AND p.type='text' WHERE m.id=? AND (SELECT count(*) FROM message_parts x WHERE x.message_id=m.id)=1`, replay.ID).Scan(&authoritative.ID, &authoritative.SessionID, &authoritative.Role, &authoritative.Status, &authoritative.Sequence, &authoritative.Text, &created); err != nil {
			rows.Close()
			return fmt.Errorf("message idempotency data invariant violation")
		}
		authoritative.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil || authoritative != replay {
			return fmt.Errorf("message idempotency data invariant violation")
		}
	}
	return nil
}

func backfillOriginFingerprints(ctx context.Context, q sqlRunner) error {
	rows, err := q.QueryContext(ctx, `SELECT id,protocol,base_url FROM providers WHERE origin_fingerprint='0000000000000000000000000000000000000000000000000000000000000000'`)
	if err != nil {
		return err
	}
	type row struct{ id, protocol, base string }
	var items []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.protocol, &r.base); err != nil {
			rows.Close()
			return err
		}
		items = append(items, r)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, r := range items {
		fp, e := provider.OriginFingerprint(provider.Protocol(r.protocol), r.base)
		if e != nil {
			return fmt.Errorf("backfill provider %q origin fingerprint: %w", r.id, e)
		}
		if _, e = q.ExecContext(ctx, `UPDATE providers SET origin_fingerprint=? WHERE id=?`, fp, r.id); e != nil {
			return e
		}
	}
	return nil
}

func migrationFiles() ([]string, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var got []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			got = append(got, e.Name())
		}
	}
	sort.Strings(got)
	if len(got) != len(manifest) {
		return nil, fmt.Errorf("migration manifest/file count mismatch")
	}
	for i, m := range manifest {
		if got[i] != m.name {
			return nil, fmt.Errorf("migration manifest mismatch at %d", i)
		}
		body, _ := migrations.Files.ReadFile(m.name)
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != m.checksum {
			return nil, fmt.Errorf("embedded migration %s checksum mismatch", m.name)
		}
	}
	return got, nil
}

func ensureMigrationJournal(ctx context.Context, q sqlRunner) error {
	if _, err := q.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL, checksum TEXT)`); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `PRAGMA table_info('schema_migrations')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	has := false
	for rows.Next() {
		var cid, nn, pk int
		var name, kind string
		var def any
		if err := rows.Scan(&cid, &name, &kind, &nn, &def, &pk); err != nil {
			return err
		}
		if name == "checksum" {
			has = true
		}
	}
	if !has {
		_, err = q.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN checksum TEXT`)
		return err
	}
	return rows.Err()
}

func validateJournal(ctx context.Context, q sqlRunner, _ []string) error {
	rows, err := q.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY rowid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		if i >= len(manifest) {
			return fmt.Errorf("unknown/future migration %q", v)
		}
		if v != manifest[i].name {
			return fmt.Errorf("missing or out-of-order migration: got %q want %q", v, manifest[i].name)
		}
		i++
	}
	// A prefix is valid (remaining migrations are applied below); gaps and extras are not.
	return rows.Err()
}

var expectedSchemaSQL = map[string]string{
	// M7 slices 6-8 (migrations 0058-0060).
	"index:ix_dbconn_kind":        "CREATE INDEX ix_dbconn_kind ON db_connections(kind)",
	"index:ix_mcpes_state":        "CREATE INDEX ix_mcpes_state ON mcp_endpoint_settings(state)",
	"index:ix_sar_root":           "CREATE INDEX ix_sar_root ON subagent_runs(root_run_id, status)",
	"table:db_connections":        "CREATE TABLE db_connections (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),\n    kind TEXT NOT NULL CHECK (kind IN ('postgres','mysql')),\n    dsn_secret_ref TEXT NOT NULL CHECK (length(dsn_secret_ref) BETWEEN 1 AND 256),\n    readonly_verified_at TEXT,\n    created_at TEXT NOT NULL,\n    created_by TEXT NOT NULL CHECK (length(created_by) BETWEEN 1 AND 128)\n)",
	"table:mcp_endpoint_settings": "CREATE TABLE mcp_endpoint_settings (\n    endpoint_id TEXT PRIMARY KEY CHECK (length(endpoint_id) BETWEEN 1 AND 128),\n    transport TEXT NOT NULL CHECK (transport IN ('stdio','https')),\n    command TEXT CHECK (command IS NULL OR length(command) BETWEEN 1 AND 512),\n    args_json TEXT CHECK (args_json IS NULL OR length(args_json) >= 2),\n    url TEXT CHECK (url IS NULL OR (length(url) BETWEEN 8 AND 2048 AND url LIKE 'https://%')),\n    origin TEXT NOT NULL CHECK (origin IN ('market','manual')),\n    source_trust TEXT NOT NULL CHECK (source_trust IN ('signed','verified','unknown')),\n    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),\n    state TEXT NOT NULL DEFAULT 'probe' CHECK (state IN ('probe','ready','degraded','revoked','quarantined')),\n    capability_digest TEXT CHECK (capability_digest IS NULL OR (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*')),\n    pinned_digest TEXT CHECK (pinned_digest IS NULL OR (length(pinned_digest) = 64 AND pinned_digest NOT GLOB '*[^0-9a-f]*')),\n    last_health_at TEXT,\n    created_at TEXT NOT NULL\n)",
	"table:mcp_market_items":      "CREATE TABLE mcp_market_items (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),\n    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 128),\n    description TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 2000),\n    transport_hint TEXT NOT NULL CHECK (transport_hint IN ('stdio','https')),\n    install_config_json TEXT NOT NULL CHECK (length(install_config_json) >= 2),\n    catalog_digest TEXT NOT NULL CHECK (length(catalog_digest) = 64 AND catalog_digest NOT GLOB '*[^0-9a-f]*'),\n    signature TEXT NOT NULL CHECK (length(signature) BETWEEN 1 AND 512),\n    fetched_at TEXT NOT NULL\n)",
	"table:subagent_observations": "CREATE TABLE subagent_observations (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subagent_run_id TEXT NOT NULL REFERENCES subagent_runs(id),\n    seq INTEGER NOT NULL CHECK (seq >= 1),\n    evidence_id TEXT NOT NULL CHECK (length(evidence_id) BETWEEN 1 AND 128),\n    summary TEXT NOT NULL CHECK (length(summary) BETWEEN 1 AND 2000),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL,\n    UNIQUE (subagent_run_id, seq)\n)",
	"table:subagent_runs":         "CREATE TABLE subagent_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    root_run_id TEXT NOT NULL CHECK (length(root_run_id) BETWEEN 1 AND 128),\n    stage_run_id TEXT CHECK (stage_run_id IS NULL OR length(stage_run_id) BETWEEN 1 AND 128),\n    purpose TEXT NOT NULL CHECK (length(purpose) BETWEEN 1 AND 2000),\n    capability_digest TEXT NOT NULL CHECK (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*'),\n    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),\n    persona_digest TEXT CHECK (persona_digest IS NULL OR (length(persona_digest) = 64 AND persona_digest NOT GLOB '*[^0-9a-f]*')),\n    status TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed','cancelled','orphaned')),\n    budget_tokens INTEGER NOT NULL CHECK (budget_tokens >= 1),\n    spent_tokens INTEGER NOT NULL DEFAULT 0 CHECK (spent_tokens >= 0),\n    deadline_ms INTEGER NOT NULL CHECK (deadline_ms >= 1),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    created_at TEXT NOT NULL,\n    completed_at TEXT,\n    UNIQUE (root_run_id, idempotency_key)\n)",
	"table:tool_call_quota":       "CREATE TABLE tool_call_quota (\n    run_id TEXT NOT NULL CHECK (length(run_id) BETWEEN 1 AND 128),\n    tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 64),\n    in_flight INTEGER NOT NULL DEFAULT 0 CHECK (in_flight >= 0),\n    calls_total INTEGER NOT NULL DEFAULT 0 CHECK (calls_total >= 0),\n    bytes_total INTEGER NOT NULL DEFAULT 0 CHECK (bytes_total >= 0),\n    updated_at TEXT NOT NULL,\n    PRIMARY KEY (run_id, tool_name)\n)",
	"table:tool_manifest_v2":      "CREATE TABLE tool_manifest_v2 (\n    tool_name TEXT PRIMARY KEY CHECK (length(tool_name) BETWEEN 1 AND 64),\n    descriptor_version TEXT NOT NULL CHECK (length(descriptor_version) BETWEEN 1 AND 32),\n    manifest_json TEXT NOT NULL CHECK (length(manifest_json) >= 2),\n    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),\n    io_semantics TEXT NOT NULL CHECK (io_semantics IN ('readonly','workspace_write')),\n    timeout_ms INTEGER NOT NULL CHECK (timeout_ms >= 1),\n    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),\n    imported_at TEXT NOT NULL\n)",
	"table:tool_results":          "CREATE TABLE tool_results (\n    run_id TEXT NOT NULL CHECK (length(run_id) BETWEEN 1 AND 128),\n    tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 64),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    result_json TEXT NOT NULL CHECK (length(result_json) >= 2),\n    result_digest TEXT NOT NULL CHECK (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL,\n    PRIMARY KEY (run_id, idempotency_key)\n)",
	"trigger:trg_mmi_readonly":    "CREATE TRIGGER trg_mmi_readonly BEFORE UPDATE ON mcp_market_items\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_sao_append_only": "CREATE TRIGGER trg_sao_append_only BEFORE UPDATE ON subagent_observations\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_sao_nodelete":    "CREATE TRIGGER trg_sao_nodelete BEFORE DELETE ON subagent_observations\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	// M8 (migrations 0061-0068).
	"index:idx_cand_subject":                                      "CREATE INDEX idx_cand_subject ON memory_candidates(subject_id, state)",
	"index:idx_cgd_subject":                                       "CREATE INDEX idx_cgd_subject ON collab_gate_decisions(subject_id, state)",
	"index:idx_cge_subject":                                       "CREATE INDEX idx_cge_subject ON collab_gate_evaluations(subject_id, outcome)",
	"index:idx_ec_subject":                                        "CREATE INDEX idx_ec_subject ON expert_catalog(subject_id, state)",
	"index:idx_es_subject":                                        "CREATE INDEX idx_es_subject ON eligibility_snapshots(subject_id, state)",
	"index:idx_fact_scope":                                        "CREATE INDEX idx_fact_scope ON memory_facts(scope_id, state)",
	"index:idx_ge_snap":                                           "CREATE INDEX idx_ge_snap ON graph_edges(snapshot_id)",
	"index:idx_gn_snap":                                           "CREATE INDEX idx_gn_snap ON graph_nodes(snapshot_id, node_type)",
	"index:idx_kbc_doc":                                           "CREATE INDEX idx_kbc_doc ON kb_chunks(document_id, document_version)",
	"index:idx_kbd_coll":                                          "CREATE INDEX idx_kbd_coll ON kb_documents(collection_id, index_state)",
	"index:idx_pb_plugin":                                         "CREATE INDEX idx_pb_plugin ON plugin_bundles(plugin_id, state)",
	"index:idx_pcb_install":                                       "CREATE INDEX idx_pcb_install ON plugin_capability_bindings(install_id, state)",
	"index:idx_pem_project":                                       "CREATE INDEX idx_pem_project ON project_phase_expert_mounting(project_id, phase_key, state)",
	"index:idx_pi_subject":                                        "CREATE INDEX idx_pi_subject ON plugin_installs(subject_id, state)",
	"index:idx_sc_state":                                          "CREATE INDEX idx_sc_state ON skill_candidates(state)",
	"index:idx_wc_state":                                          "CREATE INDEX idx_wc_state ON workflow_candidates(state)",
	"table:automation_runs":                                       "CREATE TABLE automation_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    bundle_id TEXT NOT NULL REFERENCES workflow_bundles(id),\n    state TEXT NOT NULL CHECK (state IN ('RECEIVED','POLICY_CHECKED','WAITING_CONFIRMATION','DISPATCHED','CHECKPOINTED','SUCCEEDED','COMPENSATING','QUARANTINED')),\n    approval_ref TEXT,\n    budget_json TEXT NOT NULL CHECK (length(budget_json) >= 2),\n    checkpoint_json TEXT,\n    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:candidate_evaluation_bindings":                         "CREATE TABLE candidate_evaluation_bindings (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    candidate_type TEXT NOT NULL CHECK (candidate_type IN ('skill','workflow')),\n    candidate_id TEXT NOT NULL CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    baseline_ref TEXT NOT NULL CHECK (length(baseline_ref) BETWEEN 1 AND 128),\n    environment_digest TEXT NOT NULL CHECK (length(environment_digest) = 64 AND environment_digest NOT GLOB '*[^0-9a-f]*'),\n    attestation_digest TEXT NOT NULL CHECK (length(attestation_digest) = 64 AND attestation_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:collab_gate_decisions":                                 "CREATE TABLE collab_gate_decisions (\n    decision_id TEXT PRIMARY KEY CHECK (length(decision_id) = 26 AND substr(decision_id, 1, 1) GLOB '[0-7]' AND decision_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    evaluation_id TEXT NOT NULL REFERENCES collab_gate_evaluations(evaluation_id),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    decision_token TEXT NOT NULL UNIQUE CHECK (length(decision_token) = 64 AND decision_token NOT GLOB '*[^0-9a-f]*'),\n    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),\n    capability_digest TEXT NOT NULL CHECK (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*'),\n    action TEXT NOT NULL CHECK (action IN ('enable','disable')),\n    state TEXT NOT NULL CHECK (state IN ('pending','confirmed','expired','revoked')),\n    confirmed_at TEXT,\n    expires_at TEXT NOT NULL,\n    created_at TEXT NOT NULL\n)",
	"table:collab_gate_evaluations":                               "CREATE TABLE collab_gate_evaluations (\n    evaluation_id TEXT PRIMARY KEY CHECK (length(evaluation_id) = 26 AND substr(evaluation_id, 1, 1) GLOB '[0-7]' AND evaluation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    window_start INTEGER NOT NULL CHECK (window_start >= 0),\n    window_end INTEGER NOT NULL CHECK (window_end > window_start),\n    evidence_json TEXT NOT NULL CHECK (length(evidence_json) >= 2),\n    evidence_digest TEXT NOT NULL CHECK (length(evidence_digest) = 64 AND evidence_digest NOT GLOB '*[^0-9a-f]*'),\n    criteria_version TEXT NOT NULL CHECK (length(criteria_version) BETWEEN 1 AND 64),\n    outcome TEXT NOT NULL CHECK (outcome IN ('computing','insufficient_evidence','pass','fail')),\n    failed_criteria_json TEXT,\n    created_at TEXT NOT NULL,\n    UNIQUE (subject_id, window_start, window_end, criteria_version)\n)",
	"table:device_replicas":                                       "CREATE TABLE device_replicas (\n    device_id TEXT PRIMARY KEY CHECK (length(device_id) = 26 AND substr(device_id, 1, 1) GLOB '[0-7]' AND device_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    vector_clock TEXT NOT NULL CHECK (length(vector_clock) >= 2),\n    last_ack INTEGER NOT NULL CHECK (last_ack >= 0),\n    trust_state TEXT NOT NULL CHECK (trust_state IN ('trusted','revoked')),\n    created_at TEXT NOT NULL\n)",
	"table:eligibility_snapshots":                                 "CREATE TABLE eligibility_snapshots (\n    snapshot_id TEXT PRIMARY KEY CHECK (length(snapshot_id) = 26 AND substr(snapshot_id, 1, 1) GLOB '[0-7]' AND snapshot_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    artifact_version_id TEXT NOT NULL CHECK (length(artifact_version_id) BETWEEN 1 AND 128),\n    artifact_digest TEXT NOT NULL CHECK (length(artifact_digest) = 64 AND artifact_digest NOT GLOB '*[^0-9a-f]*'),\n    review_digest TEXT NOT NULL CHECK (length(review_digest) = 64 AND review_digest NOT GLOB '*[^0-9a-f]*'),\n    gate_digest TEXT NOT NULL CHECK (length(gate_digest) = 64 AND gate_digest NOT GLOB '*[^0-9a-f]*'),\n    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),\n    classification TEXT NOT NULL CHECK (length(classification) BETWEEN 1 AND 64),\n    license_evidence TEXT NOT NULL CHECK (length(license_evidence) >= 2),\n    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 32),\n    expiry_at TEXT NOT NULL,\n    snapshot_digest TEXT NOT NULL CHECK (length(snapshot_digest) = 64 AND snapshot_digest NOT GLOB '*[^0-9a-f]*'),\n    state TEXT NOT NULL CHECK (state IN ('valid','stale','revoked','expired')),\n    created_at TEXT NOT NULL\n)",
	"table:expert_catalog":                                        "CREATE TABLE expert_catalog (\n    expert_id TEXT PRIMARY KEY CHECK (length(expert_id) = 26 AND substr(expert_id, 1, 1) GLOB '[0-7]' AND expert_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),\n    division TEXT NOT NULL CHECK (division IN ('engineering','design','product','project-management','testing','security','operations','data')),\n    source TEXT NOT NULL CHECK (source IN ('pack','local','builtin')),\n    origin_bundle_id TEXT REFERENCES plugin_bundles(bundle_id),\n    current_version_id TEXT NOT NULL CHECK (length(current_version_id) = 26 AND substr(current_version_id, 1, 1) GLOB '[0-7]' AND current_version_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    state TEXT NOT NULL CHECK (state IN ('enabled','disabled','archived')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (subject_id, name)\n)",
	"table:expert_versions":                                       "CREATE TABLE expert_versions (\n    version_id TEXT PRIMARY KEY CHECK (length(version_id) = 26 AND substr(version_id, 1, 1) GLOB '[0-7]' AND version_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    expert_id TEXT NOT NULL REFERENCES expert_catalog(expert_id),\n    semver TEXT NOT NULL CHECK (length(semver) BETWEEN 1 AND 32),\n    persona_ref TEXT NOT NULL CHECK (length(persona_ref) = 64 AND persona_ref NOT GLOB '*[^0-9a-f]*'),\n    six_section_digest TEXT NOT NULL CHECK (length(six_section_digest) = 64 AND six_section_digest NOT GLOB '*[^0-9a-f]*'),\n    change_note TEXT NOT NULL CHECK (length(change_note) BETWEEN 1 AND 2000),\n    created_at TEXT NOT NULL,\n    UNIQUE (expert_id, semver)\n)",
	"table:feedback_events":                                       "CREATE TABLE feedback_events (\n    event_id TEXT PRIMARY KEY CHECK (length(event_id) = 26 AND substr(event_id, 1, 1) GLOB '[0-7]' AND event_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    action TEXT NOT NULL CHECK (action IN ('correct','accept','reject','defer','run_result')),\n    target_type TEXT NOT NULL CHECK (length(target_type) BETWEEN 1 AND 64),\n    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 128),\n    evidence TEXT NOT NULL CHECK (length(evidence) >= 2),\n    created_at TEXT NOT NULL\n)",
	"table:graph_edges":                                           "CREATE TABLE graph_edges (\n    edge_id TEXT PRIMARY KEY CHECK (length(edge_id) = 26 AND substr(edge_id, 1, 1) GLOB '[0-7]' AND edge_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    snapshot_id TEXT NOT NULL REFERENCES ontology_snapshots(snapshot_id),\n    from_node TEXT NOT NULL REFERENCES graph_nodes(node_id),\n    to_node TEXT NOT NULL REFERENCES graph_nodes(node_id),\n    relation TEXT NOT NULL CHECK (length(relation) BETWEEN 1 AND 128),\n    provenance TEXT NOT NULL CHECK (length(provenance) >= 2),\n    created_at TEXT NOT NULL\n)",
	"table:graph_index_versions":                                  "CREATE TABLE graph_index_versions (\n    index_version TEXT PRIMARY KEY CHECK (length(index_version) = 26 AND substr(index_version, 1, 1) GLOB '[0-7]' AND index_version NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    snapshot_id TEXT NOT NULL REFERENCES ontology_snapshots(snapshot_id),\n    alias TEXT NOT NULL CHECK (length(alias) BETWEEN 1 AND 128),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    state TEXT NOT NULL CHECK (state IN ('building','verified','retired')),\n    created_at TEXT NOT NULL,\n    UNIQUE (alias, index_version)\n)",
	"table:graph_nodes":                                           "CREATE TABLE graph_nodes (\n    node_id TEXT PRIMARY KEY CHECK (length(node_id) = 26 AND substr(node_id, 1, 1) GLOB '[0-7]' AND node_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    snapshot_id TEXT NOT NULL REFERENCES ontology_snapshots(snapshot_id),\n    node_type TEXT NOT NULL CHECK (node_type IN ('File','Document','Artifact','Requirement','Decision','Module','Class','Function','Interface','Table','Field','UseCase','TestCase','Task','Release')),\n    payload TEXT NOT NULL CHECK (length(payload) BETWEEN 1 AND 65536),\n    payload_digest TEXT NOT NULL CHECK (length(payload_digest) = 64 AND payload_digest NOT GLOB '*[^0-9a-f]*'),\n    provenance TEXT NOT NULL CHECK (length(provenance) >= 2),\n    created_at TEXT NOT NULL\n)",
	"table:handoffs":                                              "CREATE TABLE handoffs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    sender TEXT NOT NULL CHECK (length(sender) BETWEEN 1 AND 128),\n    receiver TEXT NOT NULL CHECK (length(receiver) BETWEEN 1 AND 128),\n    manifest TEXT NOT NULL CHECK (length(manifest) >= 2),\n    redaction_log TEXT NOT NULL CHECK (length(redaction_log) >= 2),\n    state TEXT NOT NULL CHECK (state IN ('sent','accepted','expired')),\n    expires_at TEXT NOT NULL,\n    created_at TEXT NOT NULL\n)",
	"table:kb_chunks":                                             "CREATE TABLE kb_chunks (\n    chunk_id TEXT PRIMARY KEY CHECK (length(chunk_id) = 26 AND substr(chunk_id, 1, 1) GLOB '[0-7]' AND chunk_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    document_id TEXT NOT NULL,\n    document_version INTEGER NOT NULL,\n    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),\n    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*'),\n    locator_json TEXT NOT NULL CHECK (length(locator_json) >= 2),\n    embedding BLOB,\n    created_at TEXT NOT NULL,\n    UNIQUE (document_id, document_version, ordinal)\n)",
	"table:kb_collections":                                        "CREATE TABLE kb_collections (\n    collection_id TEXT PRIMARY KEY CHECK (length(collection_id) = 26 AND substr(collection_id, 1, 1) GLOB '[0-7]' AND collection_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),\n    auth_policy TEXT NOT NULL CHECK (length(auth_policy) BETWEEN 1 AND 512),\n    created_at TEXT NOT NULL\n)",
	"table:kb_documents":                                          "CREATE TABLE kb_documents (\n    document_id TEXT NOT NULL CHECK (length(document_id) = 26 AND substr(document_id, 1, 1) GLOB '[0-7]' AND document_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    collection_id TEXT NOT NULL REFERENCES kb_collections(collection_id),\n    version INTEGER NOT NULL CHECK (version >= 1),\n    media_type TEXT NOT NULL CHECK (length(media_type) BETWEEN 1 AND 128),\n    content_ref TEXT NOT NULL CHECK (length(content_ref) BETWEEN 1 AND 512),\n    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),\n    source_locator TEXT NOT NULL CHECK (length(source_locator) BETWEEN 1 AND 1024),\n    index_state TEXT NOT NULL CHECK (index_state IN ('pending','indexing','ready','failed')),\n    created_at TEXT NOT NULL,\n    PRIMARY KEY (document_id, version)\n)",
	"table:memory_candidates":                                     "CREATE TABLE memory_candidates (\n    candidate_id TEXT PRIMARY KEY CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    payload TEXT NOT NULL CHECK (length(payload) BETWEEN 1 AND 65536),\n    payload_digest TEXT NOT NULL CHECK (length(payload_digest) = 64 AND payload_digest NOT GLOB '*[^0-9a-f]*'),\n    inferred INTEGER NOT NULL CHECK (inferred IN (0,1)),\n    trust TEXT NOT NULL CHECK (trust IN ('untrusted','confirmed_source')),\n    state TEXT NOT NULL CHECK (state IN ('pending','confirmed','rejected','expired')),\n    confirm_token TEXT CHECK (confirm_token IS NULL OR (length(confirm_token) = 64 AND confirm_token NOT GLOB '*[^0-9a-f]*')),\n    expires_at TEXT NOT NULL,\n    created_at TEXT NOT NULL,\n    confirmed_at TEXT\n)",
	"table:memory_facts":                                          "CREATE TABLE memory_facts (\n    fact_id TEXT NOT NULL CHECK (length(fact_id) = 26 AND substr(fact_id, 1, 1) GLOB '[0-7]' AND fact_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),\n    version INTEGER NOT NULL CHECK (version >= 1),\n    sensitivity TEXT NOT NULL CHECK (sensitivity IN ('public','private','sensitive')),\n    state TEXT NOT NULL CHECK (state IN ('active','superseded','tombstoned')),\n    superseded_by TEXT,\n    deleted_at TEXT,\n    created_at TEXT NOT NULL,\n    PRIMARY KEY (fact_id, version)\n)",
	"table:memory_recall_traces":                                  "CREATE TABLE memory_recall_traces (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    query_digest TEXT NOT NULL CHECK (length(query_digest) = 64 AND query_digest NOT GLOB '*[^0-9a-f]*'),\n    hits_json TEXT NOT NULL CHECK (length(hits_json) >= 2),\n    reasons_json TEXT NOT NULL CHECK (length(reasons_json) >= 2),\n    policy_redactions_json TEXT NOT NULL CHECK (length(policy_redactions_json) >= 2),\n    created_at TEXT NOT NULL\n)",
	"table:memory_source_leaves":                                  "CREATE TABLE memory_source_leaves (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    fact_id TEXT NOT NULL,\n    fact_version INTEGER NOT NULL,\n    json_pointer TEXT NOT NULL CHECK (length(json_pointer) BETWEEN 1 AND 512),\n    evidence_ref TEXT NOT NULL CHECK (length(evidence_ref) BETWEEN 1 AND 512),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL,\n    UNIQUE (fact_id, fact_version, json_pointer),\n    FOREIGN KEY (fact_id, fact_version) REFERENCES memory_facts(fact_id, version)\n)",
	"table:memory_tombstones":                                     "CREATE TABLE memory_tombstones (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    root_ref TEXT NOT NULL CHECK (length(root_ref) BETWEEN 1 AND 128),\n    cascade_cursor TEXT NOT NULL CHECK (length(cascade_cursor) >= 2),\n    ack_set TEXT NOT NULL CHECK (length(ack_set) >= 2),\n    proof_digest TEXT CHECK (proof_digest IS NULL OR (length(proof_digest) = 64 AND proof_digest NOT GLOB '*[^0-9a-f]*')),\n    state TEXT NOT NULL CHECK (state IN ('pending','propagating','verified','compacted')),\n    created_at TEXT NOT NULL,\n    completed_at TEXT\n)",
	"table:ontology_snapshots":                                    "CREATE TABLE ontology_snapshots (\n    snapshot_id TEXT PRIMARY KEY CHECK (length(snapshot_id) = 26 AND substr(snapshot_id, 1, 1) GLOB '[0-7]' AND snapshot_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 128),\n    revision INTEGER NOT NULL CHECK (revision >= 1),\n    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64 AND content_hash NOT GLOB '*[^0-9a-f]*'),\n    source_ref TEXT NOT NULL CHECK (length(source_ref) BETWEEN 1 AND 512),\n    state TEXT NOT NULL CHECK (state IN ('active','superseded','quarantined')),\n    created_at TEXT NOT NULL,\n    UNIQUE (scope_id, revision)\n)",
	"table:plugin_bundles":                                        "CREATE TABLE plugin_bundles (\n    bundle_id TEXT PRIMARY KEY CHECK (length(bundle_id) = 26 AND substr(bundle_id, 1, 1) GLOB '[0-7]' AND bundle_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plugin_id TEXT NOT NULL CHECK (length(plugin_id) BETWEEN 1 AND 128),\n    semver TEXT NOT NULL CHECK (length(semver) BETWEEN 1 AND 32),\n    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 128),\n    kind TEXT NOT NULL CHECK (kind IN ('mcp','skill','workflow','template','tool','agent-pack')),\n    manifest_ref TEXT NOT NULL CHECK (length(manifest_ref) BETWEEN 1 AND 512),\n    entrypoint TEXT NOT NULL CHECK (length(entrypoint) BETWEEN 1 AND 512),\n    capabilities_json TEXT NOT NULL CHECK (length(capabilities_json) >= 2),\n    permissions_json TEXT NOT NULL CHECK (length(permissions_json) >= 2),\n    requires_json TEXT NOT NULL CHECK (length(requires_json) >= 2),\n    package_hash TEXT NOT NULL UNIQUE CHECK (length(package_hash) = 64 AND package_hash NOT GLOB '*[^0-9a-f]*'),\n    signature_status TEXT NOT NULL CHECK (signature_status IN ('verified','unverified','invalid')),\n    state TEXT NOT NULL CHECK (state IN ('verified','quarantined')),\n    created_at TEXT NOT NULL,\n    UNIQUE (plugin_id, semver)\n)",
	"table:plugin_capability_bindings":                            "CREATE TABLE plugin_capability_bindings (\n    binding_id TEXT PRIMARY KEY CHECK (length(binding_id) = 26 AND substr(binding_id, 1, 1) GLOB '[0-7]' AND binding_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    install_id TEXT NOT NULL REFERENCES plugin_installs(install_id),\n    target_type TEXT NOT NULL CHECK (target_type IN ('mcp_endpoint','m6_registry','tool_registry','template','persona_directory')),\n    target_id TEXT NOT NULL CHECK (length(target_id) BETWEEN 1 AND 128),\n    capability_digest TEXT NOT NULL CHECK (length(capability_digest) = 64 AND capability_digest NOT GLOB '*[^0-9a-f]*'),\n    state TEXT NOT NULL CHECK (state IN ('active','revoked')),\n    created_at TEXT NOT NULL,\n    revoked_at TEXT\n)",
	"table:plugin_installs":                                       "CREATE TABLE plugin_installs (\n    install_id TEXT PRIMARY KEY CHECK (length(install_id) = 26 AND substr(install_id, 1, 1) GLOB '[0-7]' AND install_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    bundle_id TEXT NOT NULL REFERENCES plugin_bundles(bundle_id),\n    plugin_id TEXT NOT NULL CHECK (length(plugin_id) BETWEEN 1 AND 128),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    origin TEXT NOT NULL CHECK (origin IN ('market','local','dev')),\n    state TEXT NOT NULL CHECK (state IN ('installed','enabled','disabled','quarantined','uninstalled')),\n    permission_grant_digest TEXT NOT NULL CHECK (length(permission_grant_digest) = 64 AND permission_grant_digest NOT GLOB '*[^0-9a-f]*'),\n    installed_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (subject_id, plugin_id)\n)",
	"table:project_phase_expert_mounting":                         "CREATE TABLE project_phase_expert_mounting (\n    mounting_id TEXT PRIMARY KEY CHECK (length(mounting_id) = 26 AND substr(mounting_id, 1, 1) GLOB '[0-7]' AND mounting_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL CHECK (length(project_id) = 26 AND substr(project_id, 1, 1) GLOB '[0-7]' AND project_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    phase_key TEXT NOT NULL CHECK (phase_key IN ('INITIATION_BOUNDARY','RESEARCH_EVIDENCE','REQUIREMENT_DEFINITION','SOLUTION_EXPERIENCE','ARCHITECTURE_PLAN','DEVELOPMENT_CHANGE','VERIFICATION_ACCEPTANCE','RELEASE_DELIVERY','OPERATIONS_RETROSPECTIVE')),\n    expert_id TEXT NOT NULL REFERENCES expert_catalog(expert_id),\n    version_id TEXT NOT NULL REFERENCES expert_versions(version_id),\n    state TEXT NOT NULL CHECK (state IN ('mounted','unmounted')),\n    mounted_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (project_id, phase_key, expert_id)\n)",
	"table:skill_candidates":                                      "CREATE TABLE skill_candidates (\n    candidate_id TEXT PRIMARY KEY CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    snapshot_id TEXT NOT NULL REFERENCES eligibility_snapshots(snapshot_id),\n    goal TEXT NOT NULL CHECK (length(goal) BETWEEN 1 AND 2000),\n    input_schema TEXT NOT NULL CHECK (length(input_schema) >= 2),\n    output_schema TEXT NOT NULL CHECK (length(output_schema) >= 2),\n    minimal_permissions TEXT NOT NULL CHECK (length(minimal_permissions) >= 2),\n    trigger_condition TEXT NOT NULL CHECK (length(trigger_condition) >= 2),\n    evidence_json TEXT NOT NULL CHECK (length(evidence_json) >= 2),\n    evaluation_set TEXT NOT NULL CHECK (length(evaluation_set) >= 2),\n    rollback_version TEXT,\n    state TEXT NOT NULL CHECK (state IN ('evaluating','quarantined','approved','rejected','superseded')),\n    created_at TEXT NOT NULL\n)",
	"table:sync_conflicts":                                        "CREATE TABLE sync_conflicts (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    json_pointer TEXT NOT NULL CHECK (length(json_pointer) BETWEEN 1 AND 512),\n    variants TEXT NOT NULL CHECK (length(variants) >= 2),\n    resolution TEXT,\n    state TEXT NOT NULL CHECK (state IN ('open','resolved')),\n    created_at TEXT NOT NULL\n)",
	"table:workflow_bundles":                                      "CREATE TABLE workflow_bundles (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    version INTEGER NOT NULL CHECK (version >= 1),\n    checksum TEXT NOT NULL UNIQUE CHECK (length(checksum) = 64 AND checksum NOT GLOB '*[^0-9a-f]*'),\n    permissions TEXT NOT NULL CHECK (length(permissions) >= 2),\n    rollback_ref TEXT,\n    state TEXT NOT NULL CHECK (state IN ('verified','quarantined')),\n    created_at TEXT NOT NULL\n)",
	"table:workflow_candidates":                                   "CREATE TABLE workflow_candidates (\n    candidate_id TEXT PRIMARY KEY CHECK (length(candidate_id) = 26 AND substr(candidate_id, 1, 1) GLOB '[0-7]' AND candidate_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 128),\n    snapshot_id TEXT NOT NULL REFERENCES eligibility_snapshots(snapshot_id),\n    definition_digest TEXT NOT NULL CHECK (length(definition_digest) = 64 AND definition_digest NOT GLOB '*[^0-9a-f]*'),\n    permissions TEXT NOT NULL CHECK (length(permissions) >= 2),\n    rollback_ref TEXT,\n    state TEXT NOT NULL CHECK (state IN ('evaluating','quarantined','approved','rejected','superseded')),\n    created_at TEXT NOT NULL\n)",
	"trigger:trg_cge_append_only":                                 "CREATE TRIGGER trg_cge_append_only BEFORE UPDATE ON collab_gate_evaluations\n    BEGIN SELECT RAISE(ABORT, 'M8-034'); END",
	"trigger:trg_cge_nodelete":                                    "CREATE TRIGGER trg_cge_nodelete BEFORE DELETE ON collab_gate_evaluations\n    BEGIN SELECT RAISE(ABORT, 'M8-034'); END",
	"trigger:trg_ev_append_only":                                  "CREATE TRIGGER trg_ev_append_only BEFORE UPDATE ON expert_versions\n    BEGIN SELECT RAISE(ABORT, 'M8-043'); END",
	"trigger:trg_ev_nodelete":                                     "CREATE TRIGGER trg_ev_nodelete BEFORE DELETE ON expert_versions\n    BEGIN SELECT RAISE(ABORT, 'M8-043'); END",
	"trigger:trg_mount_limit":                                     "CREATE TRIGGER trg_mount_limit BEFORE INSERT ON project_phase_expert_mounting\n    WHEN NEW.state = 'mounted' AND (SELECT COUNT(*) FROM project_phase_expert_mounting\n        WHERE project_id = NEW.project_id AND phase_key = NEW.phase_key AND state = 'mounted') >= 4\n    BEGIN SELECT RAISE(ABORT, 'M8-044'); END",
	"index:idx_sis_run":                                           "CREATE INDEX idx_sis_run ON stage_input_snapshots(stage_run_id, captured_at)",
	"index:idx_sr_active_attempt":                                 "CREATE UNIQUE INDEX idx_sr_active_attempt ON stage_runs(project_workflow_instance_id, stage_definition_id)\n    WHERE state NOT IN ('completed','cancelled')",
	"index:idx_te_down":                                           "CREATE INDEX idx_te_down ON trace_edges(from_type, from_id)",
	"index:idx_te_up":                                             "CREATE INDEX idx_te_up ON trace_edges(to_type, to_id)",
	"index:ix_ad_artifact":                                        "CREATE INDEX ix_ad_artifact ON artifact_derivations(artifact_version_id)",
	"index:ix_agent_plan_run_events_run_sequence":                 "CREATE INDEX ix_agent_plan_run_events_run_sequence ON agent_plan_run_events(run_id, sequence)",
	"index:ix_agent_plan_runs_plan_created":                       "CREATE INDEX ix_agent_plan_runs_plan_created ON agent_plan_runs(plan_id, created_at, id)",
	"index:ix_agent_plan_runs_status":                             "CREATE INDEX ix_agent_plan_runs_status ON agent_plan_runs(status)",
	"index:ix_agent_run_session_status":                           "CREATE INDEX ix_agent_run_session_status ON agent_run(session_id, status)",
	"index:ix_agent_step_turn_no":                                 "CREATE INDEX ix_agent_step_turn_no ON agent_step(turn_id, step_no)",
	"index:ix_agent_turn_run_no":                                  "CREATE INDEX ix_agent_turn_run_no ON agent_turn(run_id, turn_no)",
	"index:ix_approval_decisions_review":                          "CREATE INDEX ix_approval_decisions_review ON approval_decisions(review_id)",
	"index:ix_art_scope":                                          "CREATE INDEX ix_art_scope ON artifact_versions(scope_type, scope_id)",
	"index:ix_attachments_parse_status":                           "CREATE INDEX ix_attachments_parse_status ON attachments(parse_status) WHERE parse_status != 'succeeded'",
	"index:ix_attachments_project":                                "CREATE INDEX ix_attachments_project ON attachments(project_id, created_at DESC)",
	"index:ix_attachments_session":                                "CREATE INDEX ix_attachments_session ON attachments(session_id, created_at DESC) WHERE session_id IS NOT NULL",
	"index:ix_attachments_sha256":                                 "CREATE INDEX ix_attachments_sha256 ON attachments(sha256)",
	"index:ix_audit_aggregate_created":                            "CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC)",
	"index:ix_change_set_operation_set":                           "CREATE INDEX ix_change_set_operation_set ON change_set_operation(change_set_id, ordinal)",
	"index:ix_change_set_run_status":                              "CREATE INDEX ix_change_set_run_status ON change_set(run_id, status)",
	"index:ix_command_job_run_status":                             "CREATE INDEX ix_command_job_run_status ON command_job(run_id, status)",
	"index:ix_compaction_checkpoints_session":                     "CREATE INDEX ix_compaction_checkpoints_session ON compaction_checkpoints(session_id, version DESC)",
	"index:ix_compaction_checkpoints_status":                      "CREATE INDEX ix_compaction_checkpoints_status ON compaction_checkpoints(session_id, status)",
	"index:ix_credential_adoptions_provider":                      "CREATE INDEX ix_credential_adoptions_provider ON credential_adoptions(provider_id)",
	"index:ix_crr_cr":                                             "CREATE INDEX ix_crr_cr ON cr_revisions(cr_id, revision_no)",
	"index:ix_delegation_root":                                    "CREATE INDEX ix_delegation_root ON m6_delegation(root_id)",
	"index:ix_deletion_tombstones_status":                         "CREATE INDEX ix_deletion_tombstones_status ON deletion_tombstones(propagation_status, deleted_at)",
	"index:ix_dep_promotion":                                      "CREATE INDEX ix_dep_promotion ON deployments(promotion_id)",
	"index:ix_dt_run":                                             "CREATE INDEX ix_dt_run ON dev_tasks(stage_run_id, state)",
	"index:ix_eb_scope":                                           "CREATE INDEX ix_eb_scope ON evaluation_baselines(scope_type, scope_id)",
	"index:ix_effect_journal_run_status":                          "CREATE INDEX ix_effect_journal_run_status ON effect_journal(run_id, status)",
	"index:ix_evidence_run_captured":                              "CREATE INDEX ix_evidence_run_captured ON evidence(run_id, captured_at)",
	"index:ix_extinst_artifact":                                   "CREATE INDEX ix_extinst_artifact ON m6_extension_install(artifact_id)",
	"index:ix_extinst_subject":                                    "CREATE INDEX ix_extinst_subject ON m6_extension_install(subject, scope)",
	"index:ix_ge_run":                                             "CREATE INDEX ix_ge_run ON gate_evaluations(stage_run_id, gate_key, created_at)",
	"index:ix_governance_reviews_node":                            "CREATE INDEX ix_governance_reviews_node ON governance_reviews(node_id)",
	"index:ix_governance_reviews_plan":                            "CREATE INDEX ix_governance_reviews_plan ON governance_reviews(plan_id, created_at DESC)",
	"index:ix_handoff_capsules_dest":                              "CREATE INDEX ix_handoff_capsules_dest ON handoff_capsules(dest_session_id)",
	"index:ix_handoff_capsules_source":                            "CREATE INDEX ix_handoff_capsules_source ON handoff_capsules(source_session_id, created_at DESC)",
	"index:ix_handoff_imports_capsule":                            "CREATE INDEX ix_handoff_imports_capsule ON handoff_imports(capsule_id)",
	"index:ix_handoff_imports_target":                             "CREATE INDEX ix_handoff_imports_target ON handoff_imports(target_session_id, imported_at DESC)",
	"index:ix_idempotency_claims_expires":                         "CREATE INDEX ix_idempotency_claims_expires ON idempotency_claims(expires_at)",
	"index:ix_idempotency_expires":                                "CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at)",
	"index:ix_m5_adhoc_run":                                       "CREATE INDEX ix_m5_adhoc_run ON m5_adhoc_workspace(run_id)",
	"index:ix_m5_artifact_run":                                    "CREATE INDEX ix_m5_artifact_run ON m5_artifact(run_id, created_at)",
	"index:ix_m5_changeset_item_set":                              "CREATE INDEX ix_m5_changeset_item_set ON m5_changeset_item(changeset_id)",
	"index:ix_m5_changeset_run":                                   "CREATE INDEX ix_m5_changeset_run ON m5_changeset(run_id, created_at)",
	"index:ix_m5_conversion_run":                                  "CREATE INDEX ix_m5_conversion_run ON m5_workspace_conversion(run_id, created_at)",
	"index:ix_m5_conversion_source":                               "CREATE INDEX ix_m5_conversion_source ON m5_workspace_conversion(source_workspace_id)",
	"index:ix_m6_barrier_root":                                    "CREATE INDEX ix_m6_barrier_root ON m6_barrier(root_id)",
	"index:ix_m6_call_log_integration":                            "CREATE INDEX ix_m6_call_log_integration ON m6_call_log(integration_id, started_at)",
	"index:ix_m6_call_log_operation":                              "CREATE INDEX ix_m6_call_log_operation ON m6_call_log(operation_id, started_at)",
	"index:ix_m6_cloud_task_state":                                "CREATE INDEX ix_m6_cloud_task_state ON m6_cloud_task(state, created_at)",
	"index:ix_m6_cloudrunner_state":                               "CREATE INDEX ix_m6_cloudrunner_state ON m6_cloudrunner(state, region)",
	"index:ix_m6_complexity_decision_session":                     "CREATE INDEX ix_m6_complexity_decision_session ON m6_complexity_decision(session_id, created_at)",
	"index:ix_m6_connector_catalog_connector":                     "CREATE INDEX ix_m6_connector_catalog_connector ON m6_connector_catalog(connector_id, snapshot_version DESC)",
	"index:ix_m6_health_sample_integration":                       "CREATE INDEX ix_m6_health_sample_integration ON m6_health_sample(integration_id, sampled_at)",
	"index:ix_m6_import_candidate_state":                          "CREATE INDEX ix_m6_import_candidate_state ON m6_import_candidate(state, created_at)",
	"index:ix_m6_integration_state":                               "CREATE INDEX ix_m6_integration_state ON m6_integration(state, name)",
	"index:ix_m6_mcp_endpoint_state":                              "CREATE INDEX ix_m6_mcp_endpoint_state ON m6_mcp_endpoint(state)",
	"index:ix_m6_merge_intent_root_state":                         "CREATE INDEX ix_m6_merge_intent_root_state ON m6_merge_intent(root_id, state, sequence)",
	"index:ix_m6_reconcile_decision_task":                         "CREATE INDEX ix_m6_reconcile_decision_task ON m6_reconcile_decision(task_id, decided_at)",
	"index:ix_m6_remote_receipt_pending":                          "CREATE INDEX ix_m6_remote_receipt_pending ON m6_remote_receipt(reconcile_state, received_at)",
	"index:ix_m6_skill_install_workspace":                         "CREATE INDEX ix_m6_skill_install_workspace ON m6_skill_install(workspace_id)",
	"index:ix_m6_skill_trigger_session":                           "CREATE INDEX ix_m6_skill_trigger_session ON m6_skill_trigger(session_id, created_at)",
	"index:ix_m6_synthesis_record_root":                           "CREATE INDEX ix_m6_synthesis_record_root ON m6_synthesis_record(root_id, created_at)",
	"index:ix_m6_worker_lease_state":                              "CREATE INDEX ix_m6_worker_lease_state ON m6_worker_lease(state, expires_at)",
	"index:ix_me_promotion":                                       "CREATE INDEX ix_me_promotion ON migration_executions(promotion_id)",
	"index:ix_memories_key":                                       "CREATE INDEX ix_memories_key ON memories(project_id, key)",
	"index:ix_memories_project_layer":                             "CREATE INDEX ix_memories_project_layer ON memories(project_id, layer)",
	"index:ix_memory_revisions_memory":                            "CREATE INDEX ix_memory_revisions_memory ON memory_revisions(memory_id, created_at DESC)",
	"index:ix_memory_sources_memory":                              "CREATE INDEX ix_memory_sources_memory ON memory_sources(memory_id)",
	"index:ix_messages_session_sequence":                          "CREATE INDEX ix_messages_session_sequence ON messages(session_id, sequence)",
	"index:ix_node_run_checkpoints_run":                           "CREATE INDEX ix_node_run_checkpoints_run ON node_run_checkpoints(node_run_id, created_at DESC)",
	"index:ix_node_runs_node":                                     "CREATE INDEX ix_node_runs_node ON node_runs(node_id, attempt DESC)",
	"index:ix_node_runs_status":                                   "CREATE INDEX ix_node_runs_status ON node_runs(status)",
	"index:ix_observation_step_captured":                          "CREATE INDEX ix_observation_step_captured ON observation(step_id, captured_at)",
	"index:ix_ontology_edges_source":                              "CREATE INDEX ix_ontology_edges_source ON ontology_edges(source_node_id)",
	"index:ix_ontology_edges_target":                              "CREATE INDEX ix_ontology_edges_target ON ontology_edges(target_node_id)",
	"index:ix_ontology_nodes_project_type":                        "CREATE INDEX ix_ontology_nodes_project_type ON ontology_nodes(project_id, type)",
	"index:ix_outbox_claim":                                       "CREATE INDEX ix_outbox_claim ON outbox_events(status, available_at, lease_until)",
	"index:ix_outbox_unpub":                                       "CREATE INDEX ix_outbox_unpub ON m6_outbox(published, created_at)",
	"index:ix_plan_edges_version":                                 "CREATE INDEX ix_plan_edges_version ON plan_edges(plan_version_id)",
	"index:ix_plan_nodes_plan_sequence":                           "CREATE INDEX ix_plan_nodes_plan_sequence ON plan_nodes(plan_id, sequence)",
	"index:ix_plan_nodes_status":                                  "CREATE INDEX ix_plan_nodes_status ON plan_nodes(plan_id, status)",
	"index:ix_plan_versions_plan":                                 "CREATE INDEX ix_plan_versions_plan ON plan_versions(plan_id, version_no DESC)",
	"index:ix_plans_project_status":                               "CREATE INDEX ix_plans_project_status ON plans(project_id, status)",
	"index:ix_prm_package":                                        "CREATE INDEX ix_prm_package ON promotions(package_id)",
	"index:ix_prm_state":                                          "CREATE INDEX ix_prm_state ON promotions(state)",
	"index:ix_projects_code":                                       "CREATE UNIQUE INDEX ix_projects_code ON projects(project_code)",
	"index:ix_projects_status_created":                            "CREATE INDEX ix_projects_status_created ON projects(status, created_at, id)",
	"index:ix_provider_metadata_migration_items_credential_state": "CREATE INDEX ix_provider_metadata_migration_items_credential_state\n    ON provider_metadata_migration_items(credential_migration_state, source_fingerprint)",
	"index:ix_provider_metadata_migration_items_legacy":           "CREATE INDEX ix_provider_metadata_migration_items_legacy ON provider_metadata_migration_items(legacy_id)",
	"index:ix_provider_tests_provider_created":                    "CREATE INDEX ix_provider_tests_provider_created ON provider_tests(provider_id, created_at DESC)",
	"index:ix_rba_promotion":                                      "CREATE INDEX ix_rba_promotion ON rollback_attempts(promotion_id)",
	"index:ix_recall_events_memory":                               "CREATE INDEX ix_recall_events_memory ON recall_events(memory_id)",
	"index:ix_recall_events_session":                              "CREATE INDEX ix_recall_events_session ON recall_events(session_id, created_at DESC)",
	"index:ix_reviews_subject":                                    "CREATE INDEX ix_reviews_subject ON reviews(subject_type, subject_id, subject_version)",
	"index:ix_rm_artifact":                                        "CREATE INDEX ix_rm_artifact ON reproduction_manifests(artifact_version_id)",
	"index:ix_rp_revision":                                        "CREATE INDEX ix_rp_revision ON release_packages(cr_revision_id)",
	"index:ix_run_review_run_decided":                             "CREATE INDEX ix_run_review_run_decided ON run_review(run_id, decided_at)",
	"index:ix_run_usage_reservation_active":                       "CREATE INDEX ix_run_usage_reservation_active ON run_usage_reservation(run_id,status)",
	"index:ix_scan_task":                                          "CREATE INDEX ix_scan_task ON scan_runs(task_ref, created_at)",
	"index:ix_sessions_project_created":                           "CREATE INDEX ix_sessions_project_created ON sessions(project_id, created_at, id)",
	"index:ix_sm_subject":                                         "CREATE INDEX ix_sm_subject ON stale_marks(subject_type, subject_id)",
	"index:ix_sr_mark":                                            "CREATE INDEX ix_sr_mark ON stale_resolutions(stale_mark_id)",
	"index:ix_stages_project_phase":                               "CREATE INDEX ix_stages_project_phase ON stages(project_id, phase, id)",
	"index:ix_token_ledger_computed":                              "CREATE INDEX ix_token_ledger_computed ON token_ledger(computed_at)",
	"index:ix_token_ledger_identity":                              "CREATE INDEX ix_token_ledger_identity ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model)",
	"index:ix_token_ledger_invalidation":                          "CREATE INDEX ix_token_ledger_invalidation ON token_ledger(tokenizer_id, invalidated_at)",
	"index:ix_token_ledger_message":                               "CREATE INDEX ix_token_ledger_message ON token_ledger(message_id)",
	"index:ix_tool_call_step_status":                              "CREATE INDEX ix_tool_call_step_status ON tool_call(step_id, status)",
	"index:ix_tool_calls_run":                                     "CREATE INDEX ix_tool_calls_run ON tool_calls(node_run_id)",
	"index:ix_tr_task":                                            "CREATE INDEX ix_tr_task ON test_runs(task_ref, created_at)",
	"index:ix_updi_device":                                        "CREATE INDEX ix_updi_device ON update_installations(device_id, state)",
	"index:ix_updi_package":                                       "CREATE INDEX ix_updi_package ON update_installations(package_id)",
	"index:ix_updp_channel_state":                                 "CREATE INDEX ix_updp_channel_state ON update_packages(channel_id, state)",
	"index:ix_updr_installation":                                  "CREATE INDEX ix_updr_installation ON update_receipts(installation_id)",
	"index:ix_upra_installation":                                  "CREATE INDEX ix_upra_installation ON update_rollback_attempts(installation_id)",
	"index:ix_wfi_project":                                        "CREATE INDEX ix_wfi_project ON workflow_instances(project_id)",
	"index:ix_workspace_grant_registration_expiry":                "CREATE INDEX ix_workspace_grant_registration_expiry ON workspace_grant(registration_id, expires_at)",
	"index:ix_workspace_lease_grant_expiry":                       "CREATE INDEX ix_workspace_lease_grant_expiry ON workspace_lease(grant_id, expires_at)",
	"index:ix_workspace_registration_status":                      "CREATE INDEX ix_workspace_registration_status ON workspace_registration(status)",
	"index:ux_effect_journal_receipt":                             "CREATE UNIQUE INDEX ux_effect_journal_receipt\n    ON effect_journal(receipt_id)\n    WHERE receipt_id IS NOT NULL",
	"index:ux_m5_adhoc_root":                                      "CREATE UNIQUE INDEX ux_m5_adhoc_root ON m5_adhoc_workspace(root_canonical) WHERE state != 'deleted'",
	"index:ux_ontology_nodes_project_path":                        "CREATE UNIQUE INDEX ux_ontology_nodes_project_path ON ontology_nodes(project_id, full_path) WHERE full_path != ''",
	"index:ux_provider_default_model":                             "CREATE UNIQUE INDEX ux_provider_default_model\nON provider_models(provider_id) WHERE is_default = 1",
	"index:ux_run_review_approval_consume":                        "CREATE UNIQUE INDEX ux_run_review_approval_consume ON run_review(run_id, approval_digest, action) WHERE decision='approved'",
	"index:ux_skills_name_version":                                "CREATE UNIQUE INDEX ux_skills_name_version ON skills(name, version)",
	"index:ux_token_ledger_subject_identity_revision":             "CREATE UNIQUE INDEX ux_token_ledger_subject_identity_revision\n    ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model, tokenizer_revision)",
	"table:agent_plan_run_events":                                 "CREATE TABLE agent_plan_run_events (\n    sequence INTEGER PRIMARY KEY,\n    run_id TEXT NOT NULL REFERENCES agent_plan_runs(id) ON DELETE CASCADE,\n    type TEXT NOT NULL CHECK (type IN ('run_created','status_changed','restart_reconciled')),\n    from_status TEXT NOT NULL DEFAULT '' CHECK (from_status = '' OR from_status IN ('queued','running','joining','succeeded','failed','cancel_requested','cancelled','timed_out')),\n    to_status TEXT NOT NULL CHECK (to_status IN ('queued','running','joining','succeeded','failed','cancel_requested','cancelled','timed_out')),\n    detail TEXT NOT NULL DEFAULT '' CHECK (length(detail) <= 2048),\n    created_at TEXT NOT NULL\n)",
	"table:agent_plan_runs":                                       "CREATE TABLE agent_plan_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    parent_run_id TEXT REFERENCES agent_plan_runs(id) ON DELETE CASCADE CHECK (parent_run_id IS NULL),\n    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,\n    node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,\n    role TEXT NOT NULL CHECK (length(role) BETWEEN 1 AND 128),\n    todo_id TEXT NOT NULL CHECK (length(todo_id) = 26 AND substr(todo_id, 1, 1) GLOB '[0-7]' AND todo_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    todo_title TEXT NOT NULL CHECK (length(todo_title) BETWEEN 1 AND 200),\n    todo_description TEXT NOT NULL DEFAULT '' CHECK (length(todo_description) <= 4096),\n    todo_metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (length(todo_metadata_json) BETWEEN 2 AND 8192),\n    status TEXT NOT NULL CHECK (status IN ('queued','running','joining','succeeded','failed','cancel_requested','cancelled','timed_out')),\n    depth INTEGER NOT NULL CHECK (depth BETWEEN 0 AND 8),\n    failure TEXT NOT NULL DEFAULT '' CHECK (length(failure) <= 2048),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    terminal_at TEXT,\n    version INTEGER NOT NULL CHECK (version > 0),\n    CHECK ((status IN ('succeeded','failed','cancelled','timed_out')) = (terminal_at IS NOT NULL))\n)",
	"table:agent_run":                                             "CREATE TABLE agent_run (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,\n    status TEXT NOT NULL CHECK (status IN ('queued','running','paused_review','paused_budget','completed','failed','cancelled','interrupted','outcome_unknown')),\n    budget_json TEXT NOT NULL CHECK (json_valid(budget_json)),\n    used_json TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(used_json)),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:agent_step":                                            "CREATE TABLE agent_step (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    turn_id TEXT NOT NULL REFERENCES agent_turn(id) ON DELETE CASCADE,\n    step_no INTEGER NOT NULL CHECK (step_no > 0),\n    kind TEXT NOT NULL CHECK (kind IN ('model','tool','review')),\n    status TEXT NOT NULL CHECK (status IN ('pending','running','completed','failed')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (turn_id, step_no)\n)",
	"table:agent_turn":                                            "CREATE TABLE agent_turn (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    turn_no INTEGER NOT NULL CHECK (turn_no > 0),\n    status TEXT NOT NULL CHECK (status IN ('running','completed','failed')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (run_id, turn_no)\n)",
	"table:approval_decisions":                                    "CREATE TABLE approval_decisions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    review_id TEXT NOT NULL REFERENCES governance_reviews(id) ON DELETE CASCADE,\n    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),\n    decided_at TEXT NOT NULL,\n    UNIQUE (review_id)\n)",
	"table:artifact_derivations":                                  "CREATE TABLE artifact_derivations (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    artifact_version_id TEXT NOT NULL REFERENCES artifact_versions(id),\n    derived_from_version TEXT NOT NULL CHECK (length(derived_from_version) = 26 AND derived_from_version NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    relation TEXT NOT NULL CHECK (relation IN ('derived_from','rebuilt_from','supersedes')),\n    created_at TEXT NOT NULL\n)",
	"table:artifact_versions":                                     "CREATE TABLE artifact_versions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    artifact_id TEXT NOT NULL CHECK (length(artifact_id) BETWEEN 1 AND 256),\n    version_no INTEGER NOT NULL CHECK (version_no >= 1),\n    kind TEXT NOT NULL CHECK (kind IN ('document','patch','test_report','scan_report','package','sbom','other')),\n    scope_type TEXT NOT NULL CHECK (scope_type IN ('project','stage_run','dev_task','release','m6_root')),\n    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 256),\n    content_ref TEXT NOT NULL CHECK (length(content_ref) BETWEEN 1 AND 1024),\n    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),\n    size INTEGER NOT NULL CHECK (size >= 0),\n    media_type TEXT NOT NULL CHECK (length(media_type) BETWEEN 3 AND 256),\n    state TEXT NOT NULL CHECK (state IN ('active','superseded')),\n    created_by TEXT NOT NULL CHECK (length(created_by) BETWEEN 1 AND 128),\n    created_at TEXT NOT NULL,\n    UNIQUE (artifact_id, version_no)\n)",
	"table:attachments":                                           "CREATE TABLE attachments (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,\n    file_ref TEXT NOT NULL CHECK (length(file_ref) BETWEEN 1 AND 512),\n    original_name TEXT NOT NULL CHECK (length(original_name) BETWEEN 1 AND 256),\n    mime TEXT NOT NULL DEFAULT 'application/octet-stream' CHECK (length(mime) BETWEEN 1 AND 128),\n    size INTEGER NOT NULL CHECK (size BETWEEN 0 AND 10485760),\n    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),\n    parse_status TEXT NOT NULL DEFAULT 'pending' CHECK (parse_status IN ('pending', 'parsing', 'succeeded', 'failed')),\n    parse_error_code TEXT NOT NULL DEFAULT '' CHECK (length(parse_error_code) <= 64),\n    parsed_text TEXT NOT NULL DEFAULT '' CHECK (length(parsed_text) <= 1048576),\n    parsed_text_bytes INTEGER NOT NULL DEFAULT 0 CHECK (parsed_text_bytes BETWEEN 0 AND 1048576),\n    created_at TEXT NOT NULL,\n    deleted_at TEXT\n)",
	"table:audit_events":                                          "CREATE TABLE audit_events (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'project.updated', 'project.published', 'project.closed', 'project.reopened', 'session.created', 'session.updated', 'message.appended', 'message.rewound', 'stage.created', 'stage.updated', 'message.assistant.appended', 'memory.created', 'memory.updated', 'memory.deleted', 'agent.run.started', 'agent.run.resumed', 'agent.run.cancelled', 'agent.run.reconciled', 'review.created', 'review.status_updated', 'review.decided', 'workspace.registered', 'workspace.granted', 'workspace.leased', 'changeset.previewed', 'changeset.applied', 'changeset.reverted', 'changeset.conflicted', 'command.started', 'command.completed', 'command.failed', 'command.cancelled', 'command.reconciled', 'command.review.requested', 'web.fetched', 'web.searched', 'run.plan.updated', 'run.message.sent', 'browser.acted', 'mcp.invoked', 'plan.created', 'plan.status_updated', 'node.created', 'node.status_updated', 'ontology.node.created', 'ontology.node.updated', 'ontology.node.deleted', 'ontology.edge.created', 'ontology.edge.updated', 'ontology.edge.deleted', 'skill.created', 'skill.status_updated', 'skill.deleted', 'workspace.conversion.previewed', 'workspace.conversion.committed', 'm5.workspace.registered', 'extension.installed', 'extension.enabled', 'extension.disabled', 'extension.paused', 'extension.upgraded', 'extension.rolled_back', 'extension.uninstalled', 'mcp6.endpoint.registered', 'mcp6.endpoint.degraded', 'mcp6.endpoint.revoked', 'delegation.created', 'delegation.settled', 'barrier.created', 'barrier.arrived', 'merge.submitted', 'merge.merged', 'merge.stale', 'final.testing', 'final.completed', 'final.failed', 'stdio.worker.launched', 'stdio.worker.completed', 'stdio.worker.revoked', 'stdio.worker.expired', 'stdio.worker.recovered', 'workspace.conversion.published', 'openapi.parsed', 'integration.state.changed', 'credential.revoked', 'mapping.published', 'complexity.decided', 'synthesis.recorded', 'cloudrunner.registered', 'cloud.dispatched', 'cloud.reconciled', 'skill.import.discovered', 'skill.import.pinned', 'skill.import.inspected', 'skill.import.scanned', 'skill.import.evaluated', 'skill.import.approved', 'skill.import.rejected', 'skill.import.revoked', 'policy.created', 'policy.updated', 'policy.deactivated', 'skill.updated')),\n    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),\n    created_at TEXT NOT NULL\n)",
	"table:change_set":                                            "CREATE TABLE change_set (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    base_digest TEXT NOT NULL CHECK (length(base_digest) = 64 AND base_digest NOT GLOB '*[^0-9a-f]*'),\n    approval_digest TEXT NOT NULL CHECK (length(approval_digest) = 64 AND approval_digest NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL CHECK (status IN ('draft','previewed','approved','applied','reverted','conflicted')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:change_set_operation":                                  "CREATE TABLE change_set_operation (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    change_set_id TEXT NOT NULL REFERENCES change_set(id) ON DELETE CASCADE,\n    ordinal INTEGER NOT NULL CHECK (ordinal > 0),\n    op TEXT NOT NULL CHECK (op IN ('create','update','delete')),\n    path TEXT NOT NULL CHECK (length(path) BETWEEN 1 AND 512),\n    content TEXT,\n    content_digest TEXT CHECK (content_digest IS NULL OR (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*')),\n    original_content TEXT,\n    original_digest TEXT CHECK (original_digest IS NULL OR (length(original_digest) = 64 AND original_digest NOT GLOB '*[^0-9a-f]*')),\n    applied_digest TEXT CHECK (applied_digest IS NULL OR (length(applied_digest) = 64 AND applied_digest NOT GLOB '*[^0-9a-f]*')),\n    UNIQUE (change_set_id, ordinal)\n)",
	"table:checkpoints":                                           "CREATE TABLE checkpoints (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id),\n    snapshot_digest TEXT NOT NULL CHECK (length(snapshot_digest) = 64 AND snapshot_digest NOT GLOB '*[^0-9a-f]*'),\n    trace_root TEXT NOT NULL CHECK (length(trace_root) BETWEEN 1 AND 64),\n    sequence INTEGER NOT NULL CHECK (sequence >= 1),\n    created_at TEXT NOT NULL,\n    UNIQUE (stage_run_id, sequence)\n)",
	"table:command_job":                                           "CREATE TABLE command_job (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    command_spec_digest TEXT NOT NULL CHECK (length(command_spec_digest) = 64 AND command_spec_digest NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed','cancelled','outcome_unknown')),\n    exit_code INTEGER,\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n, backgrounded INTEGER NOT NULL DEFAULT 0 CHECK (backgrounded IN (0, 1)), pid_token TEXT, log_cursor INTEGER NOT NULL DEFAULT 0 CHECK (log_cursor >= 0), cancel_deadline TEXT)",
	"table:compaction_activation_bases":                           "CREATE TABLE compaction_activation_bases (\n    checkpoint_id TEXT PRIMARY KEY REFERENCES compaction_checkpoints(id) ON DELETE CASCADE,\n    base_revision INTEGER NOT NULL CHECK (base_revision >= 0)\n)",
	"table:compaction_activations":                                "CREATE TABLE compaction_activations (\n    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE RESTRICT,\n    checkpoint_id TEXT REFERENCES compaction_checkpoints(id) ON DELETE RESTRICT,\n    revision INTEGER NOT NULL DEFAULT 0 CHECK (revision >= 0),\n    updated_at TEXT NOT NULL\n)",
	"table:compaction_checkpoints":                                "CREATE TABLE compaction_checkpoints (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,\n    version INTEGER NOT NULL CHECK (version > 0),\n    source_start_id TEXT NOT NULL REFERENCES messages(id) ON DELETE RESTRICT,\n    source_end_id TEXT NOT NULL REFERENCES messages(id) ON DELETE RESTRICT,\n    source_start_seq INTEGER NOT NULL CHECK (source_start_seq BETWEEN 1 AND 9007199254740991),\n    source_end_seq INTEGER NOT NULL CHECK (source_end_seq BETWEEN 1 AND 9007199254740991),\n    source_digest TEXT NOT NULL CHECK (length(source_digest) = 64 AND source_digest NOT GLOB '*[^0-9a-f]*'),\n    prev_checkpoint_id TEXT REFERENCES compaction_checkpoints(id),\n    prev_checkpoint_digest TEXT CHECK (prev_checkpoint_digest IS NULL OR (length(prev_checkpoint_digest) = 64 AND prev_checkpoint_digest NOT GLOB '*[^0-9a-f]*')),\n    summary_schema_version TEXT NOT NULL DEFAULT '1.0' CHECK (length(summary_schema_version) <= 16),\n    trigger TEXT NOT NULL CHECK (trigger IN ('automatic', 'manual', 'handoff')),\n    trigger_reason TEXT NOT NULL DEFAULT '' CHECK (length(trigger_reason) <= 1024),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'superseded')),\n    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),\n    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),\n    summary_json TEXT NOT NULL DEFAULT '{}' CHECK (length(summary_json) BETWEEN 2 AND 65536),\n    human_summary TEXT NOT NULL DEFAULT '' CHECK (length(human_summary) <= 32768),\n    failure_code TEXT CHECK (failure_code IS NULL OR length(failure_code) <= 64),\n    created_at TEXT NOT NULL,\n    completed_at TEXT,\n    UNIQUE (session_id, version),\n    CHECK (source_start_seq <= source_end_seq),\n    CHECK ((status IN ('succeeded', 'failed', 'superseded')) = (completed_at IS NOT NULL))\n)",
	"table:consumed_nonces":                                       "CREATE TABLE consumed_nonces (\n    nonce TEXT PRIMARY KEY CHECK (length(nonce) BETWEEN 1 AND 128),\n    consumed_at TEXT NOT NULL\n)",
	"table:credential_adoptions":                                  "CREATE TABLE credential_adoptions (\n    credential_ref TEXT PRIMARY KEY CHECK (length(credential_ref) BETWEEN 1 AND 256),\n    provider_id TEXT NOT NULL REFERENCES providers(id),\n    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 2048),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    receipt_id TEXT NOT NULL UNIQUE CHECK (length(receipt_id) BETWEEN 1 AND 64),\n    adopted_at TEXT NOT NULL\n)",
	"table:cr_revisions":                                          "CREATE TABLE cr_revisions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    cr_id TEXT NOT NULL CHECK (length(cr_id) BETWEEN 1 AND 128),\n    revision_no INTEGER NOT NULL CHECK (revision_no >= 1),\n    manifest_json TEXT NOT NULL CHECK (length(manifest_json) >= 2),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL CHECK (status IN ('draft','submitted','approved','rejected','superseded')),\n    created_at TEXT NOT NULL,\n    UNIQUE (cr_id, revision_no)\n)",
	"table:deletion_tombstones":                                   "CREATE TABLE deletion_tombstones (\n    owner_type TEXT NOT NULL CHECK (length(owner_type) BETWEEN 1 AND 64),\n    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 64),\n    deleted_at TEXT NOT NULL,\n    propagation_status TEXT NOT NULL DEFAULT 'pending' CHECK (propagation_status IN ('pending', 'propagated', 'failed')),\n    PRIMARY KEY (owner_type, owner_id)\n)",
	"table:deployments":                                           "CREATE TABLE deployments (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    promotion_id TEXT NOT NULL REFERENCES promotions(id),\n    target_env TEXT NOT NULL CHECK (target_env IN ('dev','stage','prod')),\n    state TEXT NOT NULL CHECK (state IN ('pending','running','succeeded','failed','outcome_unknown')),\n    started_at TEXT,\n    completed_at TEXT,\n    receipt_json TEXT\n)",
	"table:dev_tasks":                                             "CREATE TABLE dev_tasks (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id),\n    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 512),\n    state TEXT NOT NULL CHECK (state IN ('draft','ready','in_progress','blocked','in_review','done','reopened','cancelled')),\n    priority TEXT NOT NULL CHECK (priority IN ('P0','P1','P2','P3')),\n    risk TEXT NOT NULL CHECK (risk IN ('low','medium','high')),\n    acceptance_digest TEXT NOT NULL CHECK (length(acceptance_digest) = 64 AND acceptance_digest NOT GLOB '*[^0-9a-f]*'),\n    assignee_id TEXT CHECK (assignee_id IS NULL OR (length(assignee_id) BETWEEN 1 AND 128)),\n    state_reason TEXT,\n    block_reason TEXT,\n    blocker_ref TEXT CHECK (blocker_ref IS NULL OR (length(blocker_ref) BETWEEN 1 AND 64)),\n    lock_version INTEGER NOT NULL DEFAULT 1 CHECK (lock_version >= 1),\n    trace_edge_id TEXT CHECK (trace_edge_id IS NULL OR (length(trace_edge_id) = 26 AND trace_edge_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),\n    created_at TEXT NOT NULL\n)",
	"table:effect_journal":                                        "CREATE TABLE effect_journal (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    effect_key TEXT NOT NULL UNIQUE CHECK (length(effect_key) BETWEEN 1 AND 256),\n    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),\n    receipt_id TEXT,\n    status TEXT NOT NULL CHECK (status IN ('prepared','committed','failed','outcome_unknown')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:evaluation_baselines":                                  "CREATE TABLE evaluation_baselines (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    scope_type TEXT NOT NULL CHECK (length(scope_type) BETWEEN 1 AND 64),\n    scope_id TEXT NOT NULL CHECK (length(scope_id) BETWEEN 1 AND 64),\n    baseline_json TEXT NOT NULL CHECK (length(baseline_json) >= 2),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:evidence":                                              "CREATE TABLE evidence (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),\n    source_uri TEXT NOT NULL CHECK (length(source_uri) BETWEEN 1 AND 2048),\n    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*'),\n    captured_at TEXT NOT NULL,\n    created_at TEXT NOT NULL\n)",
	"table:gate_evaluations":                                      "CREATE TABLE gate_evaluations (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id),\n    gate_key TEXT NOT NULL CHECK (length(gate_key) BETWEEN 1 AND 64),\n    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),\n    decision TEXT NOT NULL CHECK (decision IN ('PASS','FAIL','BLOCKED')),\n    findings_json TEXT NOT NULL CHECK (length(findings_json) >= 2),\n    created_at TEXT NOT NULL\n)",
	"table:governance_policies":                                   "CREATE TABLE governance_policies (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    version INTEGER NOT NULL CHECK (version > 0),\n    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),\n    rules_json TEXT NOT NULL CHECK (length(rules_json) BETWEEN 2 AND 65536),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:governance_reviews":                                    "CREATE TABLE governance_reviews (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_id TEXT REFERENCES plans(id),\n    node_id TEXT REFERENCES plan_nodes(id),\n    action_type TEXT NOT NULL CHECK (length(action_type) BETWEEN 1 AND 64),\n    action_digest TEXT NOT NULL CHECK (length(action_digest) = 64 AND action_digest NOT GLOB '*[^0-9a-f]*'),\n    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),\n    state_digest TEXT NOT NULL CHECK (length(state_digest) = 64 AND state_digest NOT GLOB '*[^0-9a-f]*'),\n    policy_version INTEGER NOT NULL CHECK (policy_version > 0),\n    risk_level TEXT NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'changed_after_approval')),\n    reviewer_note TEXT NOT NULL DEFAULT '' CHECK (length(reviewer_note) <= 4096),\n    expires_at TEXT,\n    created_at TEXT NOT NULL,\n    reviewed_at TEXT\n)",
	"table:handoff_capsules":                                      "CREATE TABLE handoff_capsules (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    source_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,\n    dest_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,\n    checkpoint_id TEXT NOT NULL REFERENCES compaction_checkpoints(id) ON DELETE RESTRICT,\n    active_tasks_json TEXT NOT NULL DEFAULT '[]' CHECK (length(active_tasks_json) <= 65536),\n    recent_message_ids TEXT NOT NULL DEFAULT '[]' CHECK (length(recent_message_ids) <= 65536),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'activated', 'expired', 'revoked')),\n    created_at TEXT NOT NULL,\n    activated_at TEXT,\n    expires_at TEXT\n)",
	"table:handoff_imports":                                       "CREATE TABLE handoff_imports (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    capsule_id TEXT NOT NULL REFERENCES handoff_capsules(id) ON DELETE CASCADE,\n    target_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,\n    imported_at TEXT NOT NULL,\n    UNIQUE (capsule_id, target_session_id)\n)",
	"table:idempotency_claims":                                    "CREATE TABLE idempotency_claims (\n    operation TEXT NOT NULL CHECK (operation = 'provider.model.sync'),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),\n    owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),\n    expires_at TEXT NOT NULL,\n    PRIMARY KEY (operation, idempotency_key)\n)",
	"table:idempotency_records":                                   "CREATE TABLE idempotency_records (\n    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'project.update', 'project.publish', 'project.close', 'project.reopen', 'session.create', 'session.update', 'message.append', 'message.rewind', 'stage.create', 'message.append-assistant', 'agent.run.start', 'agent.run.resume', 'agent.run.cancel', 'agent.run.reconcile', 'review.decide', 'workspace.register', 'workspace.grant', 'workspace.lease', 'changeset.preview', 'changeset.apply', 'changeset.revert', 'command.start', 'command.cancel', 'command.review.request', 'web.fetch', 'web.search', 'run.plan.put', 'run.send', 'run.cancel', 'browser.act', 'mcp.invoke', 'workspace.convert', 'extension.install', 'extension.lifecycle', 'delegation.create', 'delegation.settle', 'merge.submit', 'openapi.parse', 'complexity.decide', 'skill.import.discover', 'skill.import.inspect', 'skill.import.submit', 'skill.import.approve', 'skill.import.reject', 'skill.import.revoke')),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),\n    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),\n    created_at TEXT NOT NULL,\n    expires_at TEXT NOT NULL,\n    PRIMARY KEY (operation, idempotency_key)\n)",
	"table:m5_adhoc_workspace":                                    "CREATE TABLE m5_adhoc_workspace (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    root_canonical TEXT NOT NULL CHECK (length(root_canonical) BETWEEN 1 AND 1024),\n    display_path TEXT NOT NULL CHECK (length(display_path) BETWEEN 1 AND 1024),\n    grant_json TEXT NOT NULL CHECK (json_valid(grant_json) AND length(grant_json) BETWEEN 2 AND 16384),\n    lease_expiry TEXT NOT NULL,\n    base_digest TEXT NOT NULL CHECK (length(base_digest) = 64 AND base_digest NOT GLOB '*[^0-9a-f]*'),\n    quota_soft INTEGER NOT NULL DEFAULT 2147483648 CHECK (quota_soft > 0),\n    quota_hard INTEGER NOT NULL DEFAULT 4294967296 CHECK (quota_hard >= quota_soft),\n    used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),\n    state TEXT NOT NULL CHECK (state IN ('active','readonly_full','expiring','cleaning','cleaning_failed','retained','deleted')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m5_artifact":                                           "CREATE TABLE m5_artifact (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    mime TEXT NOT NULL CHECK (length(mime) BETWEEN 1 AND 256),\n    size INTEGER NOT NULL CHECK (size >= 0),\n    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),\n    generator TEXT NOT NULL CHECK (length(generator) BETWEEN 1 AND 128),\n    download_state TEXT NOT NULL DEFAULT 'blocked' CHECK (download_state IN ('blocked','allowed','downloaded')),\n    created_at TEXT NOT NULL\n)",
	"table:m5_changeset":                                          "CREATE TABLE m5_changeset (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    workspace_id TEXT NOT NULL REFERENCES m5_adhoc_workspace(id) ON DELETE CASCADE,\n    base_digest TEXT NOT NULL CHECK (length(base_digest) = 64 AND base_digest NOT GLOB '*[^0-9a-f]*'),\n    state TEXT NOT NULL CHECK (state IN ('staged','applied','reverted','conflict')),\n    source TEXT NOT NULL CHECK (length(source) BETWEEN 1 AND 128),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    applied_at TEXT,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m5_changeset_item":                                     "CREATE TABLE m5_changeset_item (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    changeset_id TEXT NOT NULL REFERENCES m5_changeset(id) ON DELETE CASCADE,\n    path TEXT NOT NULL CHECK (length(path) BETWEEN 1 AND 512),\n    change TEXT NOT NULL CHECK (change IN ('add','modify','delete')),\n    patch_ref TEXT NOT NULL CHECK (length(patch_ref) = 64 AND patch_ref NOT GLOB '*[^0-9a-f]*'),\n    sha256 TEXT NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),\n    size INTEGER NOT NULL CHECK (size >= 0)\n, rollback_ref TEXT CHECK (rollback_ref IS NULL OR (length(rollback_ref) = 64 AND rollback_ref NOT GLOB '*[^0-9a-f]*')))",
	"table:m5_workspace_conversion":                               "CREATE TABLE m5_workspace_conversion (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    source_workspace_id TEXT NOT NULL REFERENCES m5_adhoc_workspace(id),\n    target_project_id TEXT NOT NULL REFERENCES projects(id),\n    preview_digest TEXT NOT NULL CHECK (length(preview_digest) = 64 AND preview_digest NOT GLOB '*[^0-9a-f]*'),\n    scope_json TEXT NOT NULL CHECK (json_valid(scope_json) AND length(scope_json) BETWEEN 2 AND 16384),\n    phase TEXT NOT NULL DEFAULT 'preview' CHECK (phase IN ('preview','copying','publishing','committed','failed','abandoned')),\n    publish_journal TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(publish_journal) AND length(publish_journal) BETWEEN 2 AND 16384),\n    committed INTEGER NOT NULL DEFAULT 0 CHECK (committed IN (0, 1)),\n    committed_at TEXT,\n    audit_event_id TEXT NOT NULL CHECK (length(audit_event_id) BETWEEN 1 AND 64),\n    created_at TEXT NOT NULL,\n    CHECK ((committed = 1) = (committed_at IS NOT NULL)),\n    CHECK (committed = 0 OR phase = 'committed')\n)",
	"table:m6_api_operation":                                      "CREATE TABLE m6_api_operation (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    integration_id TEXT NOT NULL REFERENCES m6_integration(id) ON DELETE CASCADE,\n    operation_id TEXT NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 256),\n    method TEXT NOT NULL CHECK (method IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS')),\n    path_template TEXT NOT NULL CHECK (length(path_template) BETWEEN 1 AND 1024),\n    input_schema TEXT NOT NULL CHECK (json_valid(input_schema) AND length(input_schema) BETWEEN 2 AND 65536),\n    output_schema TEXT NOT NULL CHECK (json_valid(output_schema) AND length(output_schema) BETWEEN 2 AND 65536),\n    risk TEXT NOT NULL DEFAULT 'low' CHECK (risk IN ('low', 'medium', 'high')),\n    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),\n    pagination_spec TEXT CHECK (pagination_spec IS NULL OR (json_valid(pagination_spec) AND length(pagination_spec) BETWEEN 2 AND 16384)),\n    retry_spec TEXT CHECK (retry_spec IS NULL OR (json_valid(retry_spec) AND length(retry_spec) BETWEEN 2 AND 16384)),\n    idempotency_spec TEXT CHECK (idempotency_spec IS NULL OR (json_valid(idempotency_spec) AND length(idempotency_spec) BETWEEN 2 AND 16384)),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (integration_id, operation_id)\n)",
	"table:m6_barrier":                                            "CREATE TABLE m6_barrier (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    policy TEXT NOT NULL CHECK (policy IN ('ALL','QUORUM','FAIL_FAST')),\n    expected_children INTEGER NOT NULL CHECK (expected_children BETWEEN 1 AND 100),\n    quorum INTEGER CHECK (quorum IS NULL OR (quorum BETWEEN 1 AND expected_children)),\n    state TEXT NOT NULL CHECK (state IN ('open','closed')),\n    closed_reason TEXT CHECK (closed_reason IS NULL OR length(closed_reason) BETWEEN 1 AND 128),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    closed_at TEXT,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m6_barrier_arrival":                                    "CREATE TABLE m6_barrier_arrival (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    barrier_id TEXT NOT NULL REFERENCES m6_barrier(id) ON DELETE CASCADE,\n    child_id TEXT NOT NULL CHECK (length(child_id) BETWEEN 1 AND 256),\n    attempt INTEGER NOT NULL CHECK (attempt >= 0),\n    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded','failed','cancelled','expired')),\n    result_digest TEXT NOT NULL CHECK (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*'),\n    arrived_at TEXT NOT NULL,\n    UNIQUE (barrier_id, child_id)\n)",
	"table:m6_budget_account":                                     "CREATE TABLE m6_budget_account (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    dimension TEXT NOT NULL CHECK (dimension IN ('cpu_seconds','tokens','cost','wall_clock')),\n    granted INTEGER NOT NULL CHECK (granted >= 0),\n    reserved INTEGER NOT NULL DEFAULT 0 CHECK (reserved >= 0),\n    consumed INTEGER NOT NULL DEFAULT 0 CHECK (consumed >= 0),\n    refundable INTEGER NOT NULL DEFAULT 0 CHECK (refundable >= 0),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    CHECK (granted = reserved + consumed + refundable),\n    UNIQUE (root_id, dimension)\n)",
	"table:m6_call_log":                                           "CREATE TABLE m6_call_log (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    integration_id TEXT NOT NULL REFERENCES m6_integration(id),\n    operation_id TEXT NOT NULL CHECK (length(operation_id) BETWEEN 1 AND 256),\n    trace_id TEXT CHECK (trace_id IS NULL OR length(trace_id) BETWEEN 1 AND 128),\n    actor_id TEXT CHECK (actor_id IS NULL OR length(actor_id) BETWEEN 1 AND 128),\n    subject_id TEXT CHECK (subject_id IS NULL OR length(subject_id) BETWEEN 1 AND 128),\n    environment TEXT NOT NULL CHECK (environment IN ('development', 'test', 'production')),\n    grant_id TEXT CHECK (grant_id IS NULL OR length(grant_id) BETWEEN 1 AND 256),\n    attempt INTEGER NOT NULL CHECK (attempt >= 1),\n    started_at TEXT NOT NULL,\n    completed_at TEXT,\n    request_bytes INTEGER CHECK (request_bytes IS NULL OR request_bytes >= 0),\n    response_bytes INTEGER CHECK (response_bytes IS NULL OR response_bytes >= 0),\n    status_class TEXT CHECK (status_class IS NULL OR status_class IN ('1xx', '2xx', '3xx', '4xx', '5xx')),\n    request_digest TEXT CHECK (request_digest IS NULL OR (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*')),\n    response_digest TEXT CHECK (response_digest IS NULL OR (length(response_digest) = 64 AND response_digest NOT GLOB '*[^0-9a-f]*')),\n    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'cancelled', 'outcome_unknown')),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    latency_ms INTEGER CHECK (latency_ms IS NULL OR latency_ms >= 0),\n    cost_micros INTEGER CHECK (cost_micros IS NULL OR cost_micros >= 0),\n    retry_of_call_id TEXT,\n    correction_of_call_id TEXT,\n    idempotency_key_digest TEXT CHECK (idempotency_key_digest IS NULL OR (length(idempotency_key_digest) = 64 AND idempotency_key_digest NOT GLOB '*[^0-9a-f]*')),\n    policy_decision_id TEXT CHECK (policy_decision_id IS NULL OR length(policy_decision_id) BETWEEN 1 AND 256),\n    created_at TEXT NOT NULL\n)",
	"table:m6_child_manifest":                                     "CREATE TABLE m6_child_manifest (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    delegation_id TEXT NOT NULL UNIQUE REFERENCES m6_delegation(id) ON DELETE CASCADE,\n    manifest_digest TEXT NOT NULL UNIQUE CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),\n    task_scope TEXT NOT NULL CHECK (json_valid(task_scope) AND length(task_scope) BETWEEN 2 AND 16384),\n    locked_inputs TEXT NOT NULL CHECK (json_valid(locked_inputs) AND length(locked_inputs) BETWEEN 2 AND 65536),\n    budget_json TEXT NOT NULL CHECK (json_valid(budget_json) AND length(budget_json) BETWEEN 2 AND 8192),\n    capabilities TEXT NOT NULL CHECK (json_valid(capabilities) AND length(capabilities) BETWEEN 2 AND 8192),\n    created_at TEXT NOT NULL\n)",
	"table:m6_cloud_task":                                         "CREATE TABLE m6_cloud_task (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 256),\n    payload_digest TEXT NOT NULL CHECK (length(payload_digest) = 64 AND payload_digest NOT GLOB '*[^0-9a-f]*'),\n    lease_owner TEXT,\n    lease_expires_at TEXT,\n    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),\n    state TEXT NOT NULL CHECK (state IN ('created','queued','leased','running','joining',\n                                          'succeeded','failed','cancelled')),\n    result_ref TEXT,\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m6_cloudrunner":                                        "CREATE TABLE m6_cloudrunner (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    region TEXT NOT NULL CHECK (length(region) BETWEEN 1 AND 64),\n    workload_identity TEXT NOT NULL UNIQUE CHECK (length(workload_identity) BETWEEN 1 AND 256),\n    attestation_digest TEXT NOT NULL CHECK (length(attestation_digest) = 64 AND attestation_digest NOT GLOB '*[^0-9a-f]*'),\n    attestation_status TEXT NOT NULL CHECK (attestation_status IN ('verified', 'unverified', 'revoked')),\n    mtls_fingerprint TEXT NOT NULL CHECK (length(mtls_fingerprint) BETWEEN 1 AND 256),\n    capabilities TEXT NOT NULL CHECK (json_valid(capabilities) AND length(capabilities) BETWEEN 2 AND 8192),\n    state TEXT NOT NULL DEFAULT 'registered' CHECK (state IN ('registered', 'active', 'suspended', 'revoked')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m6_complexity_decision":                                "CREATE TABLE m6_complexity_decision (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 256),\n    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),\n    router_version TEXT NOT NULL CHECK (length(router_version) BETWEEN 1 AND 32),\n    tier TEXT NOT NULL CHECK (tier IN ('simple', 'moderate', 'complex', 'high-risk')),\n    routed_path TEXT NOT NULL CHECK (routed_path IN ('single', 'planned-single', 'delegated')),\n    reason_codes TEXT NOT NULL CHECK (json_valid(reason_codes) AND length(reason_codes) BETWEEN 2 AND 8192),\n    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),\n    created_at TEXT NOT NULL,\n    UNIQUE (input_digest, router_version)\n)",
	"table:m6_connector_catalog":                                  "CREATE TABLE m6_connector_catalog (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    connector_id TEXT NOT NULL CHECK (length(connector_id) BETWEEN 1 AND 128),\n    scope TEXT NOT NULL CHECK (scope IN ('personal','ad_hoc','project')),\n    snapshot_version INTEGER NOT NULL CHECK (snapshot_version > 0),\n    metadata_scope TEXT NOT NULL CHECK (metadata_scope IN ('catalog','schema','table','column','index','constraint')),\n    objects_json TEXT NOT NULL CHECK (json_valid(objects_json) AND length(objects_json) BETWEEN 2 AND 16777216),\n    fetched_at TEXT NOT NULL,\n    UNIQUE (connector_id, snapshot_version)\n)",
	"table:m6_credential_ref":                                     "CREATE TABLE m6_credential_ref (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    provider TEXT NOT NULL CHECK (length(provider) BETWEEN 1 AND 128),\n    secret_handle TEXT NOT NULL CHECK (length(secret_handle) BETWEEN 1 AND 256),\n    scopes TEXT NOT NULL CHECK (json_valid(scopes) AND length(scopes) BETWEEN 2 AND 8192),\n    expires_at TEXT,\n    revoked_at TEXT,\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (provider, secret_handle)\n)",
	"table:m6_delegation":                                         "CREATE TABLE m6_delegation (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    parent_id TEXT NOT NULL CHECK (length(parent_id) BETWEEN 1 AND 256),\n    child_task_id TEXT REFERENCES m6_cloud_task(id),\n    envelope TEXT NOT NULL CHECK (json_valid(envelope) AND length(envelope) BETWEEN 2 AND 262144),\n    envelope_digest TEXT NOT NULL UNIQUE CHECK (length(envelope_digest) = 64 AND envelope_digest NOT GLOB '*[^0-9a-f]*'),\n    nonce TEXT NOT NULL UNIQUE CHECK (length(nonce) BETWEEN 16 AND 128),\n    depth INTEGER NOT NULL CHECK (depth BETWEEN 0 AND 4),\n    state TEXT NOT NULL CHECK (state IN ('planned','grant_reserved','dispatched','arrived','settled',\n                                          'rejected','expired')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    settled_at TEXT,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (root_id, nonce)\n)",
	"table:m6_extension_artifact":                                 "CREATE TABLE m6_extension_artifact (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),\n    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 200),\n    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 64),\n    digest TEXT NOT NULL UNIQUE CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    signature_state TEXT NOT NULL CHECK (signature_state IN ('verified','failed','revoked')),\n    sbom_ref TEXT NOT NULL CHECK (length(sbom_ref) BETWEEN 1 AND 512),\n    manifest_json TEXT NOT NULL CHECK (json_valid(manifest_json) AND length(manifest_json) BETWEEN 2 AND 262144),\n    risk TEXT NOT NULL CHECK (risk IN ('low','medium','high')),\n    created_at TEXT NOT NULL,\n    UNIQUE (publisher, name, version, digest)\n)",
	"table:m6_extension_install":                                  "CREATE TABLE m6_extension_install (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    artifact_id TEXT NOT NULL REFERENCES m6_extension_artifact(id) ON DELETE CASCADE,\n    subject TEXT NOT NULL CHECK (length(subject) BETWEEN 1 AND 256),\n    scope TEXT NOT NULL CHECK (scope IN ('personal','ad_hoc','project')),\n    project_id TEXT,\n    state TEXT NOT NULL CHECK (state IN ('discovered','verifying','installed','enabled','paused','blocked',\n                                          'quarantined','uninstalled','rolling_back')),\n    permission_grant TEXT NOT NULL CHECK (json_valid(permission_grant) AND length(permission_grant) BETWEEN 2 AND 65536),\n    version INTEGER NOT NULL CHECK (version > 0),\n    installed_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= installed_at)\n)",
	"table:m6_field_mapping":                                      "CREATE TABLE m6_field_mapping (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    operation_id TEXT NOT NULL REFERENCES m6_api_operation(id) ON DELETE CASCADE,\n    source TEXT NOT NULL CHECK (length(source) BETWEEN 1 AND 512),\n    target TEXT NOT NULL CHECK (length(target) BETWEEN 1 AND 512),\n    direction TEXT NOT NULL CHECK (direction IN ('request', 'response')),\n    required INTEGER NOT NULL DEFAULT 0 CHECK (required IN (0, 1)),\n    transform_id TEXT CHECK (transform_id IS NULL OR length(transform_id) BETWEEN 1 AND 128),\n    default_value TEXT CHECK (default_value IS NULL OR (json_valid(default_value) AND length(default_value) <= 4096)),\n    schema_version INTEGER NOT NULL CHECK (schema_version > 0),\n    created_at TEXT NOT NULL,\n    UNIQUE (operation_id, source, target, direction)\n)",
	"table:m6_health_sample":                                      "CREATE TABLE m6_health_sample (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    integration_id TEXT NOT NULL REFERENCES m6_integration(id) ON DELETE CASCADE,\n    status TEXT NOT NULL CHECK (status IN ('unknown', 'healthy', 'degraded', 'unhealthy', 'paused')),\n    success INTEGER NOT NULL CHECK (success IN (0, 1)),\n    latency_ms INTEGER NOT NULL CHECK (latency_ms >= 0),\n    code_class TEXT CHECK (code_class IS NULL OR code_class IN ('1xx', '2xx', '3xx', '4xx', '5xx')),\n    sampled_at TEXT NOT NULL\n)",
	"table:m6_import_candidate":                                   "CREATE TABLE m6_import_candidate (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    asset_type TEXT NOT NULL CHECK (asset_type IN ('skill', 'profile', 'prompt_bundle')),\n    source_url TEXT NOT NULL CHECK (length(source_url) BETWEEN 1 AND 2048),\n    immutable_commit TEXT NOT NULL CHECK (length(immutable_commit) BETWEEN 1 AND 256),\n    archive_hash TEXT NOT NULL CHECK (length(archive_hash) = 64 AND archive_hash NOT GLOB '*[^0-9a-f]*'),\n    license TEXT NOT NULL CHECK (length(license) BETWEEN 1 AND 128),\n    notice_ref TEXT CHECK (notice_ref IS NULL OR length(notice_ref) BETWEEN 1 AND 512),\n    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 256),\n    signature TEXT CHECK (signature IS NULL OR length(signature) <= 8192),\n    source_attestation TEXT CHECK (source_attestation IS NULL OR (json_valid(source_attestation) AND length(source_attestation) <= 16384)),\n    scan_refs TEXT CHECK (scan_refs IS NULL OR (json_valid(scan_refs) AND length(scan_refs) <= 16384)),\n    injection_scan TEXT CHECK (injection_scan IS NULL OR (json_valid(injection_scan) AND length(injection_scan) <= 16384)),\n    evaluation_id TEXT CHECK (evaluation_id IS NULL OR length(evaluation_id) BETWEEN 1 AND 256),\n    approval TEXT CHECK (approval IS NULL OR (json_valid(approval) AND length(approval) <= 4096)),\n    state TEXT NOT NULL DEFAULT 'discovered' CHECK (state IN ('discovered', 'pinned', 'inspected', 'scanned', 'evaluated', 'awaiting_approval', 'approved', 'rejected', 'revoked')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (source_url, immutable_commit)\n)",
	"table:m6_integration":                                        "CREATE TABLE m6_integration (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),\n    kind TEXT NOT NULL CHECK (kind IN ('openapi', 'database')),\n    base_url TEXT CHECK (base_url IS NULL OR length(base_url) BETWEEN 1 AND 2048),\n    spec_digest TEXT NOT NULL CHECK (length(spec_digest) = 64 AND spec_digest NOT GLOB '*[^0-9a-f]*'),\n    spec_version TEXT NOT NULL CHECK (length(spec_version) BETWEEN 1 AND 64),\n    auth_type TEXT NOT NULL CHECK (auth_type IN ('none', 'apiKeyHeader', 'apiKeyQuery', 'bearerToken', 'basic', 'oauth2ClientCredentials')),\n    credential_ref_id TEXT REFERENCES m6_credential_ref(id),\n    direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound', 'bidirectional')),\n    role TEXT NOT NULL CHECK (role IN ('client', 'server')),\n    environment_bindings TEXT NOT NULL CHECK (json_valid(environment_bindings) AND length(environment_bindings) BETWEEN 2 AND 16384),\n    state TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'validating', 'active', 'paused', 'revoked', 'failed')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (name, spec_version)\n)",
	"table:m6_mcp_endpoint":                                       "CREATE TABLE m6_mcp_endpoint (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    transport TEXT NOT NULL CHECK (transport IN ('https','stdio')),\n    url TEXT NOT NULL CHECK (length(url) BETWEEN 8 AND 2048),\n    auth_ref TEXT NOT NULL CHECK (length(auth_ref) BETWEEN 1 AND 256),\n    capability_pin TEXT NOT NULL CHECK (json_valid(capability_pin) AND length(capability_pin) BETWEEN 2 AND 262144),\n    state TEXT NOT NULL CHECK (state IN ('registered','probe','ready','degraded','revoked','disabled')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m6_merge_intent":                                       "CREATE TABLE m6_merge_intent (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    child_id TEXT NOT NULL CHECK (length(child_id) BETWEEN 1 AND 256),\n    sequence INTEGER NOT NULL CHECK (sequence > 0),\n    expected_head TEXT NOT NULL CHECK (length(expected_head) BETWEEN 1 AND 256),\n    current_head TEXT,\n    patch_digest TEXT NOT NULL CHECK (length(patch_digest) = 64 AND patch_digest NOT GLOB '*[^0-9a-f]*'),\n    tests_ref TEXT NOT NULL CHECK (length(tests_ref) BETWEEN 1 AND 512),\n    state TEXT NOT NULL CHECK (state IN ('submitted','validating','queued','cas_check',\n                                          'applying','merged','rejected','stale','rebase_required')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (root_id, sequence)\n)",
	"table:m6_outbox":                                             "CREATE TABLE m6_outbox (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    aggregate_type TEXT NOT NULL CHECK (length(aggregate_type) BETWEEN 1 AND 64),\n    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 256),\n    event_type TEXT NOT NULL CHECK (length(event_type) BETWEEN 1 AND 128),\n    payload TEXT NOT NULL CHECK (json_valid(payload) AND length(payload) BETWEEN 2 AND 262144),\n    published INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),\n    published_at TEXT,\n    created_at TEXT NOT NULL\n)",
	"table:m6_reconcile_decision":                                 "CREATE TABLE m6_reconcile_decision (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    receipt_id TEXT NOT NULL REFERENCES m6_remote_receipt(id) ON DELETE CASCADE,\n    task_id TEXT NOT NULL REFERENCES m6_cloud_task(id) ON DELETE CASCADE,\n    decision TEXT NOT NULL CHECK (decision IN ('accepted', 'rejected', 'requeued', 'manual_review')),\n    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),\n    decided_at TEXT NOT NULL,\n    created_at TEXT NOT NULL\n)",
	"table:m6_region_policy":                                      "CREATE TABLE m6_region_policy (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    version INTEGER NOT NULL UNIQUE CHECK (version > 0),\n    allowed_regions TEXT NOT NULL CHECK (json_valid(allowed_regions) AND length(allowed_regions) BETWEEN 2 AND 16384),\n    egress_policy TEXT NOT NULL CHECK (json_valid(egress_policy) AND length(egress_policy) BETWEEN 2 AND 16384),\n    data_classification TEXT NOT NULL CHECK (json_valid(data_classification) AND length(data_classification) BETWEEN 2 AND 16384),\n    created_at TEXT NOT NULL\n)",
	"table:m6_remote_receipt":                                     "CREATE TABLE m6_remote_receipt (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    task_id TEXT NOT NULL REFERENCES m6_cloud_task(id) ON DELETE CASCADE,\n    runner_id TEXT NOT NULL REFERENCES m6_cloudrunner(id),\n    outcome TEXT NOT NULL CHECK (outcome IN ('succeeded', 'failed', 'cancelled', 'outcome_unknown')),\n    result_digest TEXT CHECK (result_digest IS NULL OR (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*')),\n    usage TEXT NOT NULL CHECK (json_valid(usage) AND length(usage) BETWEEN 2 AND 8192),\n    received_at TEXT NOT NULL,\n    reconcile_state TEXT NOT NULL DEFAULT 'pending' CHECK (reconcile_state IN ('pending', 'reconciled', 'disputed')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (task_id, runner_id, received_at)\n)",
	"table:m6_result_bundle":                                      "CREATE TABLE m6_result_bundle (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    delegation_id TEXT NOT NULL REFERENCES m6_delegation(id) ON DELETE CASCADE,\n    child_id TEXT NOT NULL CHECK (length(child_id) BETWEEN 1 AND 256),\n    attempt INTEGER NOT NULL CHECK (attempt >= 1),\n    base_head TEXT NOT NULL CHECK (length(base_head) BETWEEN 1 AND 256),\n    claims TEXT NOT NULL CHECK (json_valid(claims) AND length(claims) BETWEEN 2 AND 65536),\n    patch_digest TEXT CHECK (patch_digest IS NULL OR (length(patch_digest) = 64 AND patch_digest NOT GLOB '*[^0-9a-f]*')),\n    test_evidence TEXT NOT NULL CHECK (json_valid(test_evidence) AND length(test_evidence) BETWEEN 2 AND 65536),\n    usage TEXT NOT NULL CHECK (json_valid(usage) AND length(usage) BETWEEN 2 AND 8192),\n    risk_notes TEXT CHECK (risk_notes IS NULL OR (json_valid(risk_notes) AND length(risk_notes) <= 16384)),\n    result_digest TEXT NOT NULL UNIQUE CHECK (length(result_digest) = 64 AND result_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL,\n    UNIQUE (delegation_id, attempt)\n)",
	"table:m6_skill":                                              "CREATE TABLE m6_skill (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),\n    publisher TEXT NOT NULL CHECK (length(publisher) BETWEEN 1 AND 256),\n    status TEXT NOT NULL DEFAULT 'discovered' CHECK (status IN ('discovered', 'verified', 'installed', 'enabled', 'paused', 'quarantined', 'blocked', 'uninstalled')),\n    current_version_id TEXT CHECK (current_version_id IS NULL OR length(current_version_id) = 26),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m6_skill_dependency":                                   "CREATE TABLE m6_skill_dependency (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    skill_version_id TEXT NOT NULL REFERENCES m6_skill_version(id) ON DELETE CASCADE,\n    dependency_type TEXT NOT NULL CHECK (dependency_type IN ('skill', 'library', 'runtime')),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 256),\n    version_constraint TEXT NOT NULL CHECK (length(version_constraint) BETWEEN 1 AND 128),\n    locked_digest TEXT CHECK (locked_digest IS NULL OR (length(locked_digest) = 64 AND locked_digest NOT GLOB '*[^0-9a-f]*')),\n    created_at TEXT NOT NULL,\n    UNIQUE (skill_version_id, dependency_type, name)\n)",
	"table:m6_skill_install":                                      "CREATE TABLE m6_skill_install (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    skill_version_id TEXT NOT NULL REFERENCES m6_skill_version(id) ON DELETE CASCADE,\n    workspace_id TEXT NOT NULL CHECK (length(workspace_id) BETWEEN 1 AND 256),\n    status TEXT NOT NULL CHECK (status IN ('installed', 'enabled', 'disabled', 'quarantined')),\n    installed_at TEXT NOT NULL,\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at),\n    UNIQUE (skill_version_id, workspace_id)\n)",
	"table:m6_skill_trigger":                                      "CREATE TABLE m6_skill_trigger (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL CHECK (length(session_id) BETWEEN 1 AND 256),\n    skill_version_id TEXT NOT NULL REFERENCES m6_skill_version(id) ON DELETE CASCADE,\n    score REAL NOT NULL CHECK (score >= 0 AND score <= 1),\n    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),\n    status TEXT NOT NULL CHECK (status IN ('matched', 'executed', 'skipped', 'denied')),\n    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),\n    created_at TEXT NOT NULL\n)",
	"table:m6_skill_version":                                      "CREATE TABLE m6_skill_version (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    skill_id TEXT NOT NULL REFERENCES m6_skill(id) ON DELETE CASCADE,\n    semver TEXT NOT NULL CHECK (length(semver) BETWEEN 1 AND 64),\n    manifest_ref TEXT NOT NULL CHECK (length(manifest_ref) BETWEEN 1 AND 512),\n    package_hash TEXT NOT NULL CHECK (length(package_hash) = 64 AND package_hash NOT GLOB '*[^0-9a-f]*'),\n    signature_status TEXT NOT NULL CHECK (signature_status IN ('verified', 'unverified', 'invalid')),\n    permissions_json TEXT NOT NULL CHECK (json_valid(permissions_json) AND length(permissions_json) BETWEEN 2 AND 16384),\n    created_at TEXT NOT NULL,\n    UNIQUE (skill_id, semver)\n)",
	"table:m6_synthesis_record":                                   "CREATE TABLE m6_synthesis_record (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    root_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    barrier_id TEXT REFERENCES m6_barrier(id),\n    synthesis_digest TEXT NOT NULL UNIQUE CHECK (length(synthesis_digest) = 64 AND synthesis_digest NOT GLOB '*[^0-9a-f]*'),\n    consistent TEXT NOT NULL CHECK (json_valid(consistent) AND length(consistent) BETWEEN 2 AND 65536),\n    conflicts TEXT NOT NULL CHECK (json_valid(conflicts) AND length(conflicts) BETWEEN 2 AND 65536),\n    missing_evidence TEXT NOT NULL CHECK (json_valid(missing_evidence) AND length(missing_evidence) BETWEEN 2 AND 65536),\n    adoption_reasons TEXT NOT NULL CHECK (json_valid(adoption_reasons) AND length(adoption_reasons) BETWEEN 2 AND 65536),\n    created_at TEXT NOT NULL\n)",
	"table:m6_worker_lease":                                       "CREATE TABLE m6_worker_lease (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    runner_id TEXT NOT NULL REFERENCES m6_cloudrunner(id) ON DELETE CASCADE,\n    task_id TEXT NOT NULL UNIQUE REFERENCES m6_cloud_task(id) ON DELETE CASCADE,\n    epoch INTEGER NOT NULL CHECK (epoch > 0),\n    expires_at TEXT NOT NULL,\n    state TEXT NOT NULL CHECK (state IN ('active', 'expired', 'released', 'revoked')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:m7_audit_events":                                       "CREATE TABLE m7_audit_events (\n    seq INTEGER PRIMARY KEY CHECK (seq >= 1),\n    id TEXT NOT NULL UNIQUE CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 128),\n    resource_type TEXT NOT NULL CHECK (length(resource_type) BETWEEN 1 AND 64),\n    resource_id TEXT NOT NULL CHECK (length(resource_id) BETWEEN 1 AND 128),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    before_digest TEXT CHECK (before_digest IS NULL OR (length(before_digest) = 64 AND before_digest NOT GLOB '*[^0-9a-f]*')),\n    after_digest TEXT CHECK (after_digest IS NULL OR (length(after_digest) = 64 AND after_digest NOT GLOB '*[^0-9a-f]*')),\n    correlation_id TEXT CHECK (correlation_id IS NULL OR length(correlation_id) BETWEEN 1 AND 128),\n    prev_hash TEXT NOT NULL CHECK (length(prev_hash) = 64 AND prev_hash NOT GLOB '*[^0-9a-f]*'),\n    event_hash TEXT NOT NULL UNIQUE CHECK (length(event_hash) = 64 AND event_hash NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:memories":                                              "CREATE TABLE memories (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    layer TEXT NOT NULL CHECK (layer IN ('working', 'episodic', 'semantic', 'procedural')),\n    scope TEXT NOT NULL CHECK (scope IN ('workspace', 'project', 'session')),\n    key TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 256),\n    content TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 65536),\n    embedding_id TEXT,\n    source_id TEXT,\n    source_type TEXT CHECK (source_type IS NULL OR length(source_type) <= 64),\n    confidence REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0.0 AND confidence <= 1.0),\n    access_count INTEGER NOT NULL DEFAULT 0 CHECK (access_count >= 0),\n    last_accessed TEXT,\n    expires_at TEXT,\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:memory_revisions":                                      "CREATE TABLE memory_revisions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,\n    old_ref TEXT CHECK (old_ref IS NULL OR length(old_ref) BETWEEN 1 AND 512),\n    new_ref TEXT NOT NULL CHECK (length(new_ref) BETWEEN 1 AND 512),\n    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 1024),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    created_at TEXT NOT NULL\n)",
	"table:memory_sources":                                        "CREATE TABLE memory_sources (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,\n    source_type TEXT NOT NULL CHECK (length(source_type) BETWEEN 1 AND 64),\n    source_id TEXT NOT NULL CHECK (length(source_id) BETWEEN 1 AND 256),\n    source_revision TEXT NOT NULL DEFAULT '' CHECK (length(source_revision) <= 128),\n    quote_ref TEXT CHECK (quote_ref IS NULL OR length(quote_ref) BETWEEN 1 AND 512),\n    created_at TEXT NOT NULL\n)",
	"table:message_parts":                                         "CREATE TABLE \"message_parts\" (\n    message_id TEXT NOT NULL REFERENCES \"messages\"(id) ON DELETE CASCADE,\n    ordinal INTEGER NOT NULL CHECK (ordinal = 1),\n    type TEXT NOT NULL DEFAULT 'text' CHECK (type = 'text'),\n    text TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 16384 AND length(CAST(text AS BLOB)) <= 65536),\n    PRIMARY KEY (message_id, ordinal)\n)",
	"table:message_project_usage":                                 "CREATE TABLE message_project_usage (\n    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE RESTRICT,\n    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 67108864)\n)",
	"table:message_session_state":                                 "CREATE TABLE message_session_state (\n    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE RESTRICT,\n    last_sequence INTEGER NOT NULL CHECK (last_sequence BETWEEN 0 AND 9007199254740991),\n    message_count INTEGER NOT NULL CHECK (message_count BETWEEN 0 AND 9007199254740991),\n    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 268435456),\n    history_revision INTEGER NOT NULL DEFAULT 0 CHECK (history_revision BETWEEN 0 AND 9007199254740991),\n    CHECK (last_sequence = message_count)\n)",
	"table:message_workspace_usage":                               "CREATE TABLE message_workspace_usage (\n    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),\n    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 268435456)\n)",
	"table:messages":                                              "CREATE TABLE \"messages\" (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,\n    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'assistant', 'tool')),\n    status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('completed', 'failed')),\n    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 9007199254740991),\n    created_at TEXT NOT NULL,\n    UNIQUE (session_id, sequence)\n)",
	"table:migration_executions":                                  "CREATE TABLE migration_executions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    promotion_id TEXT NOT NULL REFERENCES promotions(id),\n    plan_digest TEXT NOT NULL CHECK (length(plan_digest) = 64 AND plan_digest NOT GLOB '*[^0-9a-f]*'),\n    state TEXT NOT NULL CHECK (state IN ('planned','applied','verified','failed')),\n    rollback_ref TEXT,\n    created_at TEXT NOT NULL\n)",
	"table:node_run_checkpoints":                                  "CREATE TABLE node_run_checkpoints (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    node_run_id TEXT NOT NULL REFERENCES node_runs(id) ON DELETE CASCADE,\n    state_ref TEXT NOT NULL CHECK (length(state_ref) BETWEEN 1 AND 512),\n    external_effect_digest TEXT NOT NULL CHECK (length(external_effect_digest) = 64 AND external_effect_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:node_runs":                                             "CREATE TABLE node_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,\n    attempt INTEGER NOT NULL CHECK (attempt > 0),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out')),\n    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT,\n    ended_at TEXT,\n    created_at TEXT NOT NULL,\n    UNIQUE (node_id, attempt),\n    CHECK (ended_at IS NULL OR started_at IS NOT NULL)\n)",
	"table:observation":                                           "CREATE TABLE observation (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    step_id TEXT NOT NULL REFERENCES agent_step(id) ON DELETE CASCADE,\n    kind TEXT NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),\n    content_digest TEXT NOT NULL CHECK (length(content_digest) = 64 AND content_digest NOT GLOB '*[^0-9a-f]*'),\n    captured_at TEXT NOT NULL,\n    created_at TEXT NOT NULL\n)",
	"table:ontology_edges":                                        "CREATE TABLE ontology_edges (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    source_node_id TEXT NOT NULL REFERENCES ontology_nodes(id) ON DELETE CASCADE,\n    target_node_id TEXT NOT NULL REFERENCES ontology_nodes(id) ON DELETE CASCADE,\n    type TEXT NOT NULL CHECK (type IN ('implements', 'extends', 'depends_on', 'references', 'contains', 'tests', 'imports', 'satisfies', 'traces', 'generates', 'configures', 'authenticates', 'authorizes')),\n    label TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 256),\n    properties_json TEXT NOT NULL DEFAULT '{}' CHECK (length(properties_json) <= 65536),\n    weight REAL NOT NULL DEFAULT 1.0 CHECK (weight >= 0.0 AND weight <= 1.0),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    CHECK (source_node_id != target_node_id)\n)",
	"table:ontology_nodes":                                        "CREATE TABLE ontology_nodes (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    type TEXT NOT NULL CHECK (type IN ('class', 'interface', 'function', 'module', 'table', 'file', 'requirement', 'artifact', 'component', 'endpoint', 'test')),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 256),\n    full_path TEXT NOT NULL DEFAULT '' CHECK (length(full_path) <= 1024),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (length(metadata_json) <= 65536),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:outbox_events":                                         "CREATE TABLE outbox_events (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    topic TEXT NOT NULL CHECK (length(topic) BETWEEN 1 AND 128),\n    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),\n    payload_json TEXT NOT NULL CHECK (length(payload_json) BETWEEN 2 AND 65536),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'completed', 'failed', 'dead_letter')),\n    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 1000),\n    available_at TEXT NOT NULL,\n    lease_owner TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),\n    lease_until TEXT,\n    last_error TEXT CHECK (last_error IS NULL OR length(last_error) BETWEEN 1 AND 2000),\n    created_at TEXT NOT NULL,\n    completed_at TEXT,\n    CHECK ((status = 'claimed') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL)),\n    CHECK ((status IN ('completed', 'failed', 'dead_letter')) = (completed_at IS NOT NULL))\n)",
	"table:plan_edges":                                            "CREATE TABLE plan_edges (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_version_id TEXT NOT NULL REFERENCES plan_versions(id) ON DELETE CASCADE,\n    from_node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,\n    to_node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,\n    condition_json TEXT NOT NULL DEFAULT '{}' CHECK (length(condition_json) BETWEEN 2 AND 8192),\n    created_at TEXT NOT NULL,\n    CHECK (from_node_id != to_node_id)\n)",
	"table:plan_nodes":                                            "CREATE TABLE plan_nodes (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,\n    parent_node_id TEXT REFERENCES plan_nodes(id),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'running', 'paused', 'completed', 'failed', 'cancelled', 'blocked')),\n    risk_level TEXT NOT NULL DEFAULT 'low' CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),\n    budget_tokens INTEGER,\n    estimate_tokens INTEGER,\n    worker_role TEXT NOT NULL DEFAULT '' CHECK (length(worker_role) <= 128),\n    sequence INTEGER NOT NULL CHECK (sequence > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (plan_id, sequence)\n)",
	"table:plan_versions":                                         "CREATE TABLE plan_versions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,\n    version_no INTEGER NOT NULL CHECK (version_no > 0),\n    graph_hash TEXT NOT NULL CHECK (length(graph_hash) = 64 AND graph_hash NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL,\n    UNIQUE (plan_id, version_no)\n)",
	"table:plans":                                                 "CREATE TABLE plans (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    stage_id TEXT,\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    version INTEGER NOT NULL CHECK (version > 0),\n    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'paused', 'completed', 'cancelled', 'failed')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:projects":                                              "CREATE TABLE projects (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200 AND name = trim(name)),\n    project_code TEXT NOT NULL CHECK (length(project_code) BETWEEN 4 AND 16 AND project_code GLOB 'ITM[0-9]*'),\n    project_type TEXT NOT NULL DEFAULT 'implementation' CHECK (project_type IN ('implementation', 'operations', 'enhancement')),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 2000),\n    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 500),\n    objective TEXT NOT NULL DEFAULT '' CHECK (length(objective) <= 2000),\n    client TEXT NOT NULL DEFAULT '' CHECK (length(client) <= 200),\n    contract_no TEXT NOT NULL DEFAULT '' CHECK (length(contract_no) <= 100),\n    amount REAL NOT NULL DEFAULT 0 CHECK (amount >= 0 AND amount <= 999999999999),\n    budget REAL NOT NULL DEFAULT 0 CHECK (budget >= 0 AND budget <= 999999999999),\n    plan_start TEXT NOT NULL DEFAULT '' CHECK (plan_start = '' OR (length(plan_start) = 10 AND plan_start GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')),\n    plan_end TEXT NOT NULL DEFAULT '' CHECK (plan_end = '' OR (length(plan_end) = 10 AND plan_end GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]')),\n    remark TEXT NOT NULL DEFAULT '' CHECK (length(remark) <= 2000),\n    close_reason TEXT NOT NULL DEFAULT '' CHECK (length(close_reason) <= 500),\n    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('created', 'active', 'closed', 'archived')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)\n)",
	"table:promotions":                                            "CREATE TABLE promotions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    package_id TEXT NOT NULL REFERENCES release_packages(id),\n    from_env TEXT NOT NULL CHECK (length(from_env) BETWEEN 1 AND 32),\n    to_env TEXT NOT NULL CHECK (to_env IN ('dev','stage','prod')),\n    canonical_intent_digest TEXT NOT NULL CHECK (length(canonical_intent_digest) = 64 AND canonical_intent_digest NOT GLOB '*[^0-9a-f]*'),\n    policy_version TEXT NOT NULL CHECK (length(policy_version) BETWEEN 1 AND 64),\n    approval_expiry TEXT,\n    state TEXT NOT NULL CHECK (state IN ('requested','policy_check','approval_check','denied','expired','migrating','deploying','validating','succeeded','failed','rolling_back','rolled_back','rollback_failed','outcome_unknown','manual')),\n    idempotency_key TEXT NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    requested_by TEXT NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 128),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:provider_metadata_migration_items":                     "CREATE TABLE provider_metadata_migration_items (\n    source_fingerprint TEXT NOT NULL REFERENCES provider_metadata_migrations(source_fingerprint) ON DELETE CASCADE,\n    item_fingerprint TEXT NOT NULL CHECK (length(item_fingerprint) = 64 AND item_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    legacy_id TEXT NOT NULL CHECK (length(legacy_id) BETWEEN 1 AND 128),\n    result TEXT NOT NULL CHECK (result IN ('imported', 'duplicate', 'conflict')),\n    provider_id TEXT,\n    detail_code TEXT NOT NULL CHECK (length(detail_code) BETWEEN 1 AND 64), credential_migration_state TEXT NOT NULL DEFAULT 'none'\n    CHECK (credential_migration_state IN ('pending', 'adopted', 'superseded', 'rejected', 'none')), credential_receipt_id TEXT\n    CHECK (credential_receipt_id IS NULL OR length(credential_receipt_id) BETWEEN 1 AND 64), credential_updated_at TEXT,\n    PRIMARY KEY (source_fingerprint, item_fingerprint)\n)",
	"table:provider_metadata_migrations":                          "CREATE TABLE provider_metadata_migrations (\n    source_fingerprint TEXT PRIMARY KEY CHECK (length(source_fingerprint) = 64 AND source_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    source_path_hash TEXT NOT NULL CHECK (length(source_path_hash) = 64 AND source_path_hash NOT GLOB '*[^0-9a-f]*'),\n    source_version TEXT NOT NULL CHECK (source_version IN ('0.1', '0.2', '0.2.1')),\n    state TEXT NOT NULL CHECK (state IN ('running', 'completed', 'failed')),\n    processed INTEGER NOT NULL DEFAULT 0 CHECK (processed >= 0),\n    total INTEGER NOT NULL DEFAULT 0 CHECK (total BETWEEN 0 AND 100),\n    imported INTEGER NOT NULL DEFAULT 0 CHECK (imported >= 0),\n    duplicates INTEGER NOT NULL DEFAULT 0 CHECK (duplicates >= 0),\n    conflicts INTEGER NOT NULL DEFAULT 0 CHECK (conflicts >= 0),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    CHECK (processed <= total),\n    CHECK (imported + duplicates + conflicts <= processed)\n)",
	"table:provider_models":                                       "CREATE TABLE provider_models (\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 200),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),\n    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),\n    position INTEGER NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 49), context_window INTEGER,\n    PRIMARY KEY (provider_id, model_id),\n    UNIQUE (provider_id, position)\n)",
	"table:provider_tests":                                        "CREATE TABLE provider_tests (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT,\n    completed_at TEXT,\n    created_at TEXT NOT NULL,\n    CHECK (completed_at IS NULL OR started_at IS NOT NULL)\n)",
	"table:providers":                                             "CREATE TABLE providers (\n    id TEXT PRIMARY KEY,\n    legacy_id TEXT UNIQUE,\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),\n    credential_ref TEXT CHECK (credential_ref IS NULL OR length(credential_ref) BETWEEN 1 AND 500),\n    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable', 'requires_reentry')),\n    status TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),\n    deleted_at TEXT, origin_fingerprint TEXT NOT NULL\n    DEFAULT '0000000000000000000000000000000000000000000000000000000000000000'\n    CHECK (length(origin_fingerprint) = 64 AND origin_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    CHECK ((credential_ref IS NOT NULL) = (credential_state = 'configured'))\n)",
	"table:recall_events":                                         "CREATE TABLE recall_events (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,\n    query_hash TEXT NOT NULL CHECK (length(query_hash) = 64 AND query_hash NOT GLOB '*[^0-9a-f]*'),\n    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,\n    score REAL NOT NULL CHECK (score >= 0.0 AND score <= 1.0),\n    rank INTEGER NOT NULL CHECK (rank > 0),\n    injected_tokens INTEGER NOT NULL DEFAULT 0 CHECK (injected_tokens >= 0),\n    created_at TEXT NOT NULL\n)",
	"table:release_blobs":                                         "CREATE TABLE release_blobs (\n    digest TEXT PRIMARY KEY CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    content TEXT NOT NULL CHECK (length(content) >= 2),\n    created_at TEXT NOT NULL\n)",
	"table:release_packages":                                      "CREATE TABLE release_packages (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    cr_revision_id TEXT NOT NULL REFERENCES cr_revisions(id),\n    manifest_digest TEXT NOT NULL CHECK (length(manifest_digest) = 64 AND manifest_digest NOT GLOB '*[^0-9a-f]*'),\n    blob_digest TEXT NOT NULL CHECK (length(blob_digest) = 64 AND blob_digest NOT GLOB '*[^0-9a-f]*'),\n    signature TEXT NOT NULL CHECK (length(signature) BETWEEN 1 AND 512),\n    state TEXT NOT NULL CHECK (state IN ('building','sealed','failed')),\n    created_at TEXT NOT NULL,\n    sealed_at TEXT\n)",
	"table:reproduction_manifests":                                "CREATE TABLE reproduction_manifests (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    artifact_version_id TEXT NOT NULL REFERENCES artifact_versions(id),\n    manifest_json TEXT NOT NULL CHECK (length(manifest_json) >= 2),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:reviews":                                               "CREATE TABLE reviews (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_type TEXT NOT NULL CHECK (length(subject_type) BETWEEN 1 AND 64),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 64),\n    subject_version INTEGER NOT NULL CHECK (subject_version >= 1),\n    verdict TEXT NOT NULL CHECK (verdict IN ('approve','reject')),\n    reviewer_id TEXT NOT NULL CHECK (length(reviewer_id) BETWEEN 1 AND 128),\n    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2000),\n    created_at TEXT NOT NULL\n)",
	"table:rollback_attempts":                                     "CREATE TABLE rollback_attempts (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    promotion_id TEXT NOT NULL REFERENCES promotions(id),\n    dimension TEXT NOT NULL CHECK (dimension IN ('binary','schema','data','external')),\n    state TEXT NOT NULL CHECK (state IN ('pending','running','succeeded','failed')),\n    plan_digest TEXT NOT NULL CHECK (length(plan_digest) = 64 AND plan_digest NOT GLOB '*[^0-9a-f]*'),\n    operator_id TEXT NOT NULL CHECK (length(operator_id) BETWEEN 1 AND 128),\n    result_json TEXT NOT NULL CHECK (length(result_json) >= 2),\n    created_at TEXT NOT NULL,\n    completed_at TEXT\n)",
	"table:run_event":                                             "CREATE TABLE run_event (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    sequence INTEGER NOT NULL CHECK (sequence > 0),\n    event_type TEXT NOT NULL CHECK (length(event_type) BETWEEN 1 AND 128),\n    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),\n    created_at TEXT NOT NULL,\n    UNIQUE (run_id, sequence)\n)",
	"table:run_plan":                                              "CREATE TABLE run_plan (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE UNIQUE,\n    plan_digest TEXT NOT NULL CHECK (length(plan_digest) = 64 AND plan_digest NOT GLOB '*[^0-9a-f]*'),\n    content_json TEXT NOT NULL CHECK (json_valid(content_json)),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:run_review":                                            "CREATE TABLE run_review (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    approval_digest TEXT NOT NULL CHECK (length(approval_digest) = 64 AND approval_digest NOT GLOB '*[^0-9a-f]*'),\n    decision TEXT NOT NULL CHECK (decision IN ('approved','rejected')),\n    decided_by TEXT NOT NULL CHECK (length(decided_by) BETWEEN 1 AND 128),\n    decided_at TEXT NOT NULL,\n    created_at TEXT NOT NULL\n, action TEXT NOT NULL DEFAULT '', resource_digest TEXT NOT NULL DEFAULT '', base_digest TEXT NOT NULL DEFAULT '', config_digest TEXT NOT NULL DEFAULT '', policy_digest TEXT NOT NULL DEFAULT '', descriptor_digest TEXT NOT NULL DEFAULT '', consumed_at TEXT)",
	"table:run_usage_reservation":                                 "CREATE TABLE run_usage_reservation (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26),\n    run_id TEXT NOT NULL REFERENCES agent_run(id) ON DELETE CASCADE,\n    reserved_json TEXT NOT NULL CHECK (json_valid(reserved_json)),\n    committed_json TEXT CHECK (committed_json IS NULL OR json_valid(committed_json)),\n    status TEXT NOT NULL CHECK (status IN ('reserved','committed','released')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:scan_runs":                                             "CREATE TABLE scan_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    task_ref TEXT NOT NULL CHECK (length(task_ref) BETWEEN 1 AND 64),\n    scanner TEXT NOT NULL CHECK (length(scanner) BETWEEN 1 AND 128),\n    severity_gate TEXT NOT NULL CHECK (length(severity_gate) BETWEEN 1 AND 32),\n    report_digest TEXT NOT NULL CHECK (length(report_digest) = 64 AND report_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:schema_migrations":                                     "CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL, checksum TEXT)",
	"table:sessions":                                              "CREATE TABLE sessions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200 AND title = trim(title)),\n    status TEXT NOT NULL DEFAULT 'active' CHECK (status = 'active'),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version = 1)\n, pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1)), revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0))",
	"table:skills":                                                "CREATE TABLE skills (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 32),\n    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'deprecated', 'disabled')),\n    permissions_json TEXT NOT NULL CHECK (length(permissions_json) BETWEEN 2 AND 2048),\n    entry_point TEXT NOT NULL CHECK (length(entry_point) BETWEEN 1 AND 512),\n    manifest_json TEXT NOT NULL CHECK (length(manifest_json) BETWEEN 2 AND 65536),\n    signature TEXT CHECK (signature IS NULL OR length(signature) <= 1024),\n    publisher_id TEXT,\n    min_engine_version TEXT CHECK (min_engine_version IS NULL OR length(min_engine_version) <= 32),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:stage_definitions":                                     "CREATE TABLE stage_definitions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    workflow_version_id TEXT NOT NULL REFERENCES workflow_versions(id) ON DELETE CASCADE,\n    stage_key TEXT NOT NULL CHECK (stage_key IN ('INITIATION_BOUNDARY','RESEARCH_EVIDENCE','REQUIREMENT_DEFINITION',\n                                                  'SOLUTION_EXPERIENCE','ARCHITECTURE_PLAN','DEVELOPMENT_CHANGE',\n                                                  'VERIFICATION_ACCEPTANCE','RELEASE_DELIVERY','OPERATIONS_RETROSPECTIVE')),\n    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 1 AND 9),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),\n    dependency_keys TEXT NOT NULL CHECK (json_valid(dependency_keys)),\n    gate_policy TEXT NOT NULL CHECK (json_valid(gate_policy)),\n    UNIQUE (workflow_version_id, stage_key),\n    UNIQUE (workflow_version_id, ordinal)\n)",
	"table:stage_input_snapshots":                                 "CREATE TABLE stage_input_snapshots (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    stage_run_id TEXT NOT NULL REFERENCES stage_runs(id) ON DELETE CASCADE,\n    inputs_json TEXT NOT NULL CHECK (json_valid(inputs_json) AND length(inputs_json) BETWEEN 2 AND 1048576),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    captured_at TEXT NOT NULL\n)",
	"table:stage_runs":                                            "CREATE TABLE stage_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_workflow_instance_id TEXT NOT NULL REFERENCES workflow_instances(id) ON DELETE CASCADE,\n    stage_definition_id TEXT NOT NULL REFERENCES stage_definitions(id),\n    attempt_no INTEGER NOT NULL CHECK (attempt_no >= 1),\n    state TEXT NOT NULL CHECK (state IN ('draft','ready','running','waiting_review','approved','completed',\n                                          'blocked','paused','cancelled')),\n    lock_version INTEGER NOT NULL DEFAULT 1 CHECK (lock_version > 0),\n    started_at TEXT,\n    completed_at TEXT,\n    created_at TEXT NOT NULL,\n    UNIQUE (project_workflow_instance_id, stage_definition_id, attempt_no)\n)",
	"table:stages":                                                "CREATE TABLE stages (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    phase INTEGER NOT NULL CHECK (phase BETWEEN 1 AND 9),\n    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200 AND title = trim(title)),\n    status TEXT NOT NULL DEFAULT 'not_started' CHECK (status IN ('not_started', 'in_progress', 'waiting_review', 'approved', 'completed', 'rejected', 'stale', 'paused', 'blocked', 'cancelled')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),\n    UNIQUE (project_id, phase)\n)",
	"table:stale_marks":                                           "CREATE TABLE stale_marks (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    subject_type TEXT NOT NULL CHECK (length(subject_type) BETWEEN 1 AND 64),\n    subject_id TEXT NOT NULL CHECK (length(subject_id) BETWEEN 1 AND 64),\n    cause_edge TEXT NOT NULL CHECK (length(cause_edge) BETWEEN 1 AND 64),\n    detected_at TEXT NOT NULL\n)",
	"table:stale_resolutions":                                     "CREATE TABLE stale_resolutions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    stale_mark_id TEXT NOT NULL REFERENCES stale_marks(id),\n    resolution_type TEXT NOT NULL CHECK (resolution_type IN ('recaptured','reevaluated','waived')),\n    reevaluation_id TEXT CHECK (reevaluation_id IS NULL OR (length(reevaluation_id) = 26 AND reevaluation_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),\n    resolved_by TEXT NOT NULL CHECK (length(resolved_by) BETWEEN 1 AND 128),\n    resolved_at TEXT NOT NULL\n)",
	"table:test_runs":                                             "CREATE TABLE test_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    task_ref TEXT NOT NULL CHECK (length(task_ref) BETWEEN 1 AND 64),\n    result TEXT NOT NULL CHECK (result IN ('pass','fail','error','timeout')),\n    report_digest TEXT NOT NULL CHECK (length(report_digest) = 64 AND report_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:token_ledger":                                          "CREATE TABLE token_ledger (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,\n    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),\n    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),\n    tokenizer_revision TEXT NOT NULL DEFAULT '' CHECK (length(tokenizer_revision) <= 64),\n    token_count INTEGER NOT NULL CHECK (token_count >= 0),\n    estimation_method TEXT NOT NULL CHECK (estimation_method IN ('char-ratio', 'tiktoken', 'provider-reported', 'manual')),\n    utf8_bytes INTEGER NOT NULL CHECK (utf8_bytes >= 0),\n    computed_at TEXT NOT NULL,\n    subject_type TEXT NOT NULL DEFAULT 'message'\n        CHECK (subject_type IN ('message', 'message_part', 'tool_result', 'summary', 'injected_instruction')),\n    subject_id TEXT NOT NULL DEFAULT '',\n    tokenizer_id TEXT NOT NULL DEFAULT 'lunitide-canonical-v1'\n        CHECK (length(tokenizer_id) > 0 AND length(tokenizer_id) <= 128),\n    invalidated_at TEXT\n)",
	"table:tool_call":                                             "CREATE TABLE tool_call (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    step_id TEXT NOT NULL REFERENCES agent_step(id) ON DELETE CASCADE,\n    tool_name TEXT NOT NULL CHECK (length(tool_name) BETWEEN 1 AND 128),\n    args_digest TEXT NOT NULL CHECK (length(args_digest) = 64 AND args_digest NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL CHECK (status IN ('proposed','policy_checked','awaiting_review','approved','running','succeeded','failed','denied','cancelled','outcome_unknown')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:tool_calls":                                            "CREATE TABLE tool_calls (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    node_run_id TEXT NOT NULL REFERENCES node_runs(id) ON DELETE CASCADE,\n    tool_id TEXT NOT NULL CHECK (length(tool_id) BETWEEN 1 AND 128),\n    args_hash TEXT NOT NULL CHECK (length(args_hash) = 64 AND args_hash NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),\n    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),\n    risk TEXT NOT NULL DEFAULT 'low' CHECK (risk IN ('low', 'medium', 'high', 'critical')),\n    approval_id TEXT REFERENCES governance_reviews(id),\n    created_at TEXT NOT NULL\n)",
	"table:trace_edges":                                           "CREATE TABLE trace_edges (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    from_type TEXT NOT NULL CHECK (length(from_type) BETWEEN 1 AND 64),\n    from_id TEXT NOT NULL CHECK (length(from_id) BETWEEN 1 AND 64),\n    from_digest TEXT NOT NULL CHECK (length(from_digest) = 64 AND from_digest NOT GLOB '*[^0-9a-f]*'),\n    relation TEXT NOT NULL CHECK (relation IN ('implements','verifies','traces_to','derived_from','reviews','produces','promotes')),\n    to_type TEXT NOT NULL CHECK (length(to_type) BETWEEN 1 AND 64),\n    to_id TEXT NOT NULL CHECK (length(to_id) BETWEEN 1 AND 64),\n    to_digest TEXT NOT NULL CHECK (length(to_digest) = 64 AND to_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:update_channels":                                       "CREATE TABLE update_channels (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL UNIQUE CHECK (name IN ('stable','beta')),\n    state TEXT NOT NULL CHECK (state IN ('active','retired')),\n    created_at TEXT NOT NULL\n)",
	"table:update_installations":                                  "CREATE TABLE update_installations (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    package_id TEXT NOT NULL REFERENCES update_packages(id),\n    device_id TEXT NOT NULL CHECK (length(device_id) BETWEEN 1 AND 128),\n    state TEXT NOT NULL CHECK (state IN ('pending','downloading','installing','succeeded','failed','rolled_back')),\n    created_at TEXT NOT NULL,\n    completed_at TEXT\n)",
	"table:update_packages":                                       "CREATE TABLE update_packages (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    channel_id TEXT NOT NULL REFERENCES update_channels(id),\n    app_version TEXT NOT NULL CHECK (length(app_version) BETWEEN 1 AND 32),\n    min_version TEXT NOT NULL CHECK (length(min_version) BETWEEN 1 AND 32),\n    package_digest TEXT NOT NULL CHECK (length(package_digest) = 64 AND package_digest NOT GLOB '*[^0-9a-f]*'),\n    signature TEXT NOT NULL CHECK (length(signature) BETWEEN 1 AND 512),\n    nonce TEXT NOT NULL CHECK (length(nonce) BETWEEN 1 AND 128),\n    not_before TEXT NOT NULL,\n    expires_at TEXT NOT NULL,\n    key_id TEXT NOT NULL CHECK (length(key_id) BETWEEN 1 AND 64),\n    state TEXT NOT NULL CHECK (state IN ('building','published','revoked')),\n    created_at TEXT NOT NULL,\n    UNIQUE (channel_id, app_version)\n)",
	"table:update_receipts":                                       "CREATE TABLE update_receipts (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    installation_id TEXT NOT NULL REFERENCES update_installations(id),\n    receipt_json TEXT NOT NULL CHECK (length(receipt_json) >= 2),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"table:update_rollback_attempts":                              "CREATE TABLE update_rollback_attempts (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    installation_id TEXT NOT NULL REFERENCES update_installations(id),\n    state TEXT NOT NULL CHECK (state IN ('pending','running','succeeded','failed')),\n    operator_id TEXT NOT NULL CHECK (length(operator_id) BETWEEN 1 AND 128),\n    result_json TEXT NOT NULL CHECK (length(result_json) >= 2),\n    created_at TEXT NOT NULL,\n    completed_at TEXT\n)",
	"table:workflow_instances":                                    "CREATE TABLE workflow_instances (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,\n    workflow_version_id TEXT NOT NULL REFERENCES workflow_versions(id),\n    state TEXT NOT NULL CHECK (state IN ('running','completed','cancelled')),\n    created_at TEXT NOT NULL,\n    completed_at TEXT,\n    CHECK (completed_at IS NULL OR completed_at >= created_at)\n)",
	"table:workflow_versions":                                     "CREATE TABLE workflow_versions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,\n    version INTEGER NOT NULL CHECK (version > 0),\n    status TEXT NOT NULL CHECK (status IN ('draft','published')),\n    definition_digest TEXT NOT NULL CHECK (length(definition_digest) = 64 AND definition_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL,\n    published_at TEXT,\n    UNIQUE (project_id, version)\n)",
	"table:workspace_grant":                                       "CREATE TABLE workspace_grant (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    registration_id TEXT NOT NULL REFERENCES workspace_registration(id) ON DELETE CASCADE,\n    scope_json TEXT NOT NULL CHECK (json_valid(scope_json)),\n    expires_at TEXT NOT NULL,\n    status TEXT NOT NULL CHECK (status IN ('active','expired','revoked')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:workspace_lease":                                       "CREATE TABLE workspace_lease (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    grant_id TEXT NOT NULL REFERENCES workspace_grant(id) ON DELETE CASCADE,\n    fencing_token INTEGER NOT NULL CHECK (fencing_token > 0),\n    expires_at TEXT NOT NULL,\n    status TEXT NOT NULL CHECK (status IN ('active','expired','released')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"table:workspace_registration":                                "CREATE TABLE workspace_registration (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    canonical_root TEXT NOT NULL UNIQUE CHECK (length(canonical_root) BETWEEN 1 AND 1024),\n    root_digest TEXT NOT NULL CHECK (length(root_digest) = 64 AND root_digest NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL CHECK (status IN ('active','revoked')),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL CHECK (updated_at >= created_at)\n)",
	"trigger:providers_credential_ref_insert":                     "CREATE TRIGGER providers_credential_ref_insert\nBEFORE INSERT ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256\nBEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END",
	"trigger:providers_credential_ref_update":                     "CREATE TRIGGER providers_credential_ref_update\nBEFORE UPDATE OF credential_ref ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256\nBEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END",
	"trigger:trg_art_immutable_delete":                            "CREATE TRIGGER trg_art_immutable_delete BEFORE DELETE ON artifact_versions\n    BEGIN SELECT RAISE(ABORT, 'M7-ART-001'); END",
	"trigger:trg_art_immutable_update":                            "CREATE TRIGGER trg_art_immutable_update BEFORE UPDATE ON artifact_versions\n    BEGIN SELECT RAISE(ABORT, 'M7-ART-001'); END",
	"trigger:trg_crr_frozen":                                      "CREATE TRIGGER trg_crr_frozen BEFORE UPDATE ON cr_revisions\n    WHEN OLD.status <> 'draft' AND (NEW.cr_id <> OLD.cr_id OR NEW.revision_no <> OLD.revision_no\n        OR NEW.manifest_json <> OLD.manifest_json OR NEW.digest <> OLD.digest)\n    BEGIN SELECT RAISE(ABORT, 'M7-REV-002'); END",
	"trigger:trg_crr_nodelete":                                    "CREATE TRIGGER trg_crr_nodelete BEFORE DELETE ON cr_revisions\n    WHEN OLD.status <> 'draft'\n    BEGIN SELECT RAISE(ABORT, 'M7-REV-002'); END",
	"trigger:trg_evd_ad_ro_d":                                     "CREATE TRIGGER trg_evd_ad_ro_d BEFORE DELETE ON artifact_derivations\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_ad_ro_u":                                     "CREATE TRIGGER trg_evd_ad_ro_u BEFORE UPDATE ON artifact_derivations\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_ck_ro_d":                                     "CREATE TRIGGER trg_evd_ck_ro_d BEFORE DELETE ON checkpoints\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_ck_ro_u":                                     "CREATE TRIGGER trg_evd_ck_ro_u BEFORE UPDATE ON checkpoints\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_eb_ro_d":                                     "CREATE TRIGGER trg_evd_eb_ro_d BEFORE DELETE ON evaluation_baselines\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_eb_ro_u":                                     "CREATE TRIGGER trg_evd_eb_ro_u BEFORE UPDATE ON evaluation_baselines\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_ge_ro_d":                                     "CREATE TRIGGER trg_evd_ge_ro_d BEFORE DELETE ON gate_evaluations\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_ge_ro_u":                                     "CREATE TRIGGER trg_evd_ge_ro_u BEFORE UPDATE ON gate_evaluations\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_reviews_ro_d":                                "CREATE TRIGGER trg_evd_reviews_ro_d BEFORE DELETE ON reviews\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_reviews_ro_u":                                "CREATE TRIGGER trg_evd_reviews_ro_u BEFORE UPDATE ON reviews\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_rm_ro_d":                                     "CREATE TRIGGER trg_evd_rm_ro_d BEFORE DELETE ON reproduction_manifests\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_rm_ro_u":                                     "CREATE TRIGGER trg_evd_rm_ro_u BEFORE UPDATE ON reproduction_manifests\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_sc_ro_d":                                     "CREATE TRIGGER trg_evd_sc_ro_d BEFORE DELETE ON scan_runs\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_sc_ro_u":                                     "CREATE TRIGGER trg_evd_sc_ro_u BEFORE UPDATE ON scan_runs\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_sm_ro_d":                                     "CREATE TRIGGER trg_evd_sm_ro_d BEFORE DELETE ON stale_marks\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_sm_ro_u":                                     "CREATE TRIGGER trg_evd_sm_ro_u BEFORE UPDATE ON stale_marks\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_sr_ro_d":                                     "CREATE TRIGGER trg_evd_sr_ro_d BEFORE DELETE ON stale_resolutions\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_sr_ro_u":                                     "CREATE TRIGGER trg_evd_sr_ro_u BEFORE UPDATE ON stale_resolutions\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_te_ro_d":                                     "CREATE TRIGGER trg_evd_te_ro_d BEFORE DELETE ON trace_edges\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_te_ro_u":                                     "CREATE TRIGGER trg_evd_te_ro_u BEFORE UPDATE ON trace_edges\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_tr_ro_d":                                     "CREATE TRIGGER trg_evd_tr_ro_d BEFORE DELETE ON test_runs\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_evd_tr_ro_u":                                     "CREATE TRIGGER trg_evd_tr_ro_u BEFORE UPDATE ON test_runs\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_m6_cl_ro_d":                                      "CREATE TRIGGER trg_m6_cl_ro_d BEFORE DELETE ON m6_call_log\n    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END",
	"trigger:trg_m6_cl_ro_u":                                      "CREATE TRIGGER trg_m6_cl_ro_u BEFORE UPDATE ON m6_call_log\n    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END",
	"trigger:trg_m6_hs_ro_d":                                      "CREATE TRIGGER trg_m6_hs_ro_d BEFORE DELETE ON m6_health_sample\n    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END",
	"trigger:trg_m6_hs_ro_u":                                      "CREATE TRIGGER trg_m6_hs_ro_u BEFORE UPDATE ON m6_health_sample\n    BEGIN SELECT RAISE(ABORT, 'M6-APPENDONLY'); END",
	"trigger:trg_m7ae_immutable_u":                                "CREATE TRIGGER trg_m7ae_immutable_u BEFORE UPDATE ON m7_audit_events\n    BEGIN SELECT RAISE(ABORT, 'M7-DR-001'); END",
	"trigger:trg_m7ae_nodelete":                                   "CREATE TRIGGER trg_m7ae_nodelete BEFORE DELETE ON m7_audit_events\n    BEGIN SELECT RAISE(ABORT, 'M7-DR-001'); END",
	"trigger:trg_pkg_sealed_ro_d":                                 "CREATE TRIGGER trg_pkg_sealed_ro_d BEFORE DELETE ON release_packages\n    WHEN OLD.state = 'sealed'\n    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END",
	"trigger:trg_pkg_sealed_ro_u":                                 "CREATE TRIGGER trg_pkg_sealed_ro_u BEFORE UPDATE ON release_packages\n    WHEN OLD.state = 'sealed'\n    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END",
	"trigger:trg_rb_immutable_d":                                  "CREATE TRIGGER trg_rb_immutable_d BEFORE DELETE ON release_blobs\n    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END",
	"trigger:trg_rb_immutable_u":                                  "CREATE TRIGGER trg_rb_immutable_u BEFORE UPDATE ON release_blobs\n    BEGIN SELECT RAISE(ABORT, 'M7-PKG-001'); END",
	"trigger:trg_rba_nodelete":                                    "CREATE TRIGGER trg_rba_nodelete BEFORE DELETE ON rollback_attempts\n    BEGIN SELECT RAISE(ABORT, 'M7-RBK-002'); END",
	"trigger:trg_updr_immutable_u":                                "CREATE TRIGGER trg_updr_immutable_u BEFORE UPDATE ON update_receipts\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_updr_nodelete":                                   "CREATE TRIGGER trg_updr_nodelete BEFORE DELETE ON update_receipts\n    BEGIN SELECT RAISE(ABORT, 'M7-EVD-001'); END",
	"trigger:trg_upra_nodelete":                                   "CREATE TRIGGER trg_upra_nodelete BEFORE DELETE ON update_rollback_attempts\n    BEGIN SELECT RAISE(ABORT, 'M7-RBK-002'); END",
	"trigger:trg_wv_published_readonly":                           "CREATE TRIGGER trg_wv_published_readonly BEFORE UPDATE ON workflow_versions\n    WHEN OLD.status = 'published' AND NEW.definition_digest <> OLD.definition_digest\n    BEGIN SELECT RAISE(ABORT, 'M7-WF-001'); END",
	// M9 (migration 0069).
	"index:idx_ie_principal":     "CREATE INDEX idx_ie_principal ON identity_events(principal_id, binding_version)",
	"index:idx_pr_org":           "CREATE INDEX idx_pr_org ON principals(org_id, state)",
	"index:idx_rb_scope":         "CREATE INDEX idx_rb_scope ON role_bindings(org_id, principal_id, state)",
	"index:idx_ts_org":           "CREATE INDEX idx_ts_org ON team_spaces(org_id, state)",
	"table:identity_events":      "CREATE TABLE identity_events (\n    event_id TEXT PRIMARY KEY CHECK (length(event_id) = 26 AND substr(event_id, 1, 1) GLOB '[0-7]' AND event_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    org_id TEXT NOT NULL REFERENCES organizations(org_id),\n    principal_id TEXT NOT NULL REFERENCES principals(principal_id),\n    kind TEXT NOT NULL CHECK (kind IN ('created','bound','rebound','suspended','restored','expired','revoked')),\n    binding_version INTEGER NOT NULL CHECK (binding_version >= 1),\n    created_at TEXT NOT NULL\n)",
	"table:organizations":        "CREATE TABLE organizations (\n    org_id TEXT PRIMARY KEY CHECK (length(org_id) = 26 AND substr(org_id, 1, 1) GLOB '[0-7]' AND org_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL UNIQUE CHECK (length(name) BETWEEN 1 AND 128),\n    state TEXT NOT NULL CHECK (state IN ('draft','active','suspended','closed')),\n    retention_days INTEGER NOT NULL DEFAULT 730 CHECK (retention_days >= 90),\n    residency_policy_digest TEXT CHECK (residency_policy_digest IS NULL OR (length(residency_policy_digest) = 64 AND residency_policy_digest NOT GLOB '*[^0-9a-f]*')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:principals":           "CREATE TABLE principals (\n    principal_id TEXT PRIMARY KEY CHECK (length(principal_id) = 26 AND substr(principal_id, 1, 1) GLOB '[0-7]' AND principal_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    org_id TEXT NOT NULL REFERENCES organizations(org_id),\n    external_id TEXT CHECK (external_id IS NULL OR length(external_id) BETWEEN 1 AND 256),\n    idp_issuer TEXT CHECK (idp_issuer IS NULL OR length(idp_issuer) BETWEEN 8 AND 512),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 128),\n    state TEXT NOT NULL CHECK (state IN ('active','suspended','expired','revoked')),\n    binding_version INTEGER NOT NULL DEFAULT 1 CHECK (binding_version >= 1),\n    expires_at TEXT,\n    revoked_at TEXT,\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (org_id, external_id)\n)",
	"table:role_bindings":        "CREATE TABLE role_bindings (\n    binding_id TEXT PRIMARY KEY CHECK (length(binding_id) = 26 AND substr(binding_id, 1, 1) GLOB '[0-7]' AND binding_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    org_id TEXT NOT NULL REFERENCES organizations(org_id),\n    principal_id TEXT NOT NULL REFERENCES principals(principal_id),\n    scope_key TEXT NOT NULL CHECK (scope_key = 'org' OR length(scope_key) = 26),\n    role TEXT NOT NULL CHECK (role IN ('org-admin','space-admin','operator','approver','auditor','legal-officer','member')),\n    expires_at TEXT NOT NULL,\n    state TEXT NOT NULL CHECK (state IN ('active','revoked')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (org_id, principal_id, scope_key, role)\n)",
	"table:team_spaces":          "CREATE TABLE team_spaces (\n    space_id TEXT PRIMARY KEY CHECK (length(space_id) = 26 AND substr(space_id, 1, 1) GLOB '[0-7]' AND space_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    org_id TEXT NOT NULL REFERENCES organizations(org_id),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),\n    state TEXT NOT NULL CHECK (state IN ('active','archived')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (org_id, name)\n)",
	"trigger:trg_ie_append_only":  "CREATE TRIGGER trg_ie_append_only BEFORE UPDATE ON identity_events\n    BEGIN SELECT RAISE(ABORT, 'M9-003'); END",
	"trigger:trg_ie_nodelete":     "CREATE TRIGGER trg_ie_nodelete BEFORE DELETE ON identity_events\n    BEGIN SELECT RAISE(ABORT, 'M9-003'); END",
	"trigger:trg_ie_org_immutable": "CREATE TRIGGER trg_ie_org_immutable BEFORE UPDATE ON identity_events\n    WHEN NEW.org_id <> OLD.org_id\n    BEGIN SELECT RAISE(ABORT, 'M9-003'); END",
	"trigger:trg_org_org_immutable": "CREATE TRIGGER trg_org_org_immutable BEFORE UPDATE ON organizations\n    WHEN NEW.org_id <> OLD.org_id\n    BEGIN SELECT RAISE(ABORT, 'M9-003'); END",
	"trigger:trg_pr_org_immutable": "CREATE TRIGGER trg_pr_org_immutable BEFORE UPDATE ON principals\n    WHEN NEW.org_id <> OLD.org_id\n    BEGIN SELECT RAISE(ABORT, 'M9-003'); END",
	"trigger:trg_rb_org_immutable": "CREATE TRIGGER trg_rb_org_immutable BEFORE UPDATE ON role_bindings\n    WHEN NEW.org_id <> OLD.org_id\n    BEGIN SELECT RAISE(ABORT, 'M9-003'); END",
	"trigger:trg_ts_org_immutable": "CREATE TRIGGER trg_ts_org_immutable BEFORE UPDATE ON team_spaces\n    WHEN NEW.org_id <> OLD.org_id\n    BEGIN SELECT RAISE(ABORT, 'M9-003'); END",
	// M10 (migration 0071+).
	"index:idx_nom_state":        "CREATE INDEX idx_nom_state ON memory_nominations(state, created_at)",
	"table:memory_nominations":   "CREATE TABLE memory_nominations (\n    nomination_id TEXT PRIMARY KEY CHECK (length(nomination_id) = 26 AND substr(nomination_id, 1, 1) GLOB '[0-7]' AND nomination_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    candidate_id TEXT NOT NULL REFERENCES memory_candidates(candidate_id),\n    nominator TEXT NOT NULL CHECK (length(nominator) BETWEEN 1 AND 128),\n    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),\n    source_session_id TEXT CHECK (source_session_id IS NULL OR (length(source_session_id) = 26 AND substr(source_session_id, 1, 1) GLOB '[0-7]' AND source_session_id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*')),\n    state TEXT NOT NULL DEFAULT 'nominated' CHECK (state IN ('nominated','decided','withdrawn')),\n    decided_at TEXT,\n    created_at TEXT NOT NULL,\n    UNIQUE (candidate_id)\n)",
}

type columnSpec struct {
	name, kind, def     string
	notNull, pk, hidden int
}

var expectedColumns = map[string][]columnSpec{
	"providers":                         {{"id", "TEXT", "", 0, 1, 0}, {"legacy_id", "TEXT", "", 0, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"protocol", "TEXT", "", 1, 0, 0}, {"base_url", "TEXT", "", 1, 0, 0}, {"credential_ref", "TEXT", "", 0, 0, 0}, {"credential_state", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'enabled'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}, {"deleted_at", "TEXT", "", 0, 0, 0}, {"origin_fingerprint", "TEXT", "'0000000000000000000000000000000000000000000000000000000000000000'", 1, 0, 0}},
	"provider_models":                   {{"provider_id", "TEXT", "", 1, 1, 0}, {"model_id", "TEXT", "", 1, 2, 0}, {"display_name", "TEXT", "", 1, 0, 0}, {"is_default", "INTEGER", "0", 1, 0, 0}, {"position", "INTEGER", "0", 1, 0, 0}, {"context_window", "INTEGER", "", 0, 0, 0}},
	"schema_migrations":                 {{"version", "TEXT", "", 0, 1, 0}, {"applied_at", "TEXT", "", 1, 0, 0}, {"checksum", "TEXT", "", 0, 0, 0}},
	"provider_metadata_migrations":      {{"source_fingerprint", "TEXT", "", 0, 1, 0}, {"source_path_hash", "TEXT", "", 1, 0, 0}, {"source_version", "TEXT", "", 1, 0, 0}, {"state", "TEXT", "", 1, 0, 0}, {"processed", "INTEGER", "0", 1, 0, 0}, {"total", "INTEGER", "0", 1, 0, 0}, {"imported", "INTEGER", "0", 1, 0, 0}, {"duplicates", "INTEGER", "0", 1, 0, 0}, {"conflicts", "INTEGER", "0", 1, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"provider_metadata_migration_items": {{"source_fingerprint", "TEXT", "", 1, 1, 0}, {"item_fingerprint", "TEXT", "", 1, 2, 0}, {"legacy_id", "TEXT", "", 1, 0, 0}, {"result", "TEXT", "", 1, 0, 0}, {"provider_id", "TEXT", "", 0, 0, 0}, {"detail_code", "TEXT", "", 1, 0, 0}, {"credential_migration_state", "TEXT", "'none'", 1, 0, 0}, {"credential_receipt_id", "TEXT", "", 0, 0, 0}, {"credential_updated_at", "TEXT", "", 0, 0, 0}},
	"provider_tests":                    {{"id", "TEXT", "", 0, 1, 0}, {"provider_id", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "", 1, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 0, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"idempotency_records":               {{"operation", "TEXT", "", 1, 1, 0}, {"idempotency_key", "TEXT", "", 1, 2, 0}, {"request_digest", "TEXT", "", 1, 0, 0}, {"response_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"expires_at", "TEXT", "", 1, 0, 0}},
	"idempotency_claims":                {{"operation", "TEXT", "", 1, 1, 0}, {"idempotency_key", "TEXT", "", 1, 2, 0}, {"request_digest", "TEXT", "", 1, 0, 0}, {"owner", "TEXT", "", 1, 0, 0}, {"expires_at", "TEXT", "", 1, 0, 0}},
	"outbox_events":                     {{"id", "TEXT", "", 0, 1, 0}, {"topic", "TEXT", "", 1, 0, 0}, {"aggregate_id", "TEXT", "", 1, 0, 0}, {"payload_json", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"attempts", "INTEGER", "0", 1, 0, 0}, {"available_at", "TEXT", "", 1, 0, 0}, {"lease_owner", "TEXT", "", 0, 0, 0}, {"lease_until", "TEXT", "", 0, 0, 0}, {"last_error", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}},
	"audit_events":                      {{"id", "TEXT", "", 0, 1, 0}, {"action", "TEXT", "", 1, 0, 0}, {"aggregate_id", "TEXT", "", 1, 0, 0}, {"actor", "TEXT", "", 1, 0, 0}, {"metadata_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"credential_adoptions":              {{"credential_ref", "TEXT", "", 0, 1, 0}, {"provider_id", "TEXT", "", 1, 0, 0}, {"origin", "TEXT", "", 1, 0, 0}, {"protocol", "TEXT", "", 1, 0, 0}, {"receipt_id", "TEXT", "", 1, 0, 0}, {"adopted_at", "TEXT", "", 1, 0, 0}},
	"projects":                          {{"id", "TEXT", "", 0, 1, 0}, {"name", "TEXT", "", 1, 0, 0}, {"project_code", "TEXT", "", 1, 0, 0}, {"project_type", "TEXT", "'implementation'", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"summary", "TEXT", "''", 1, 0, 0}, {"objective", "TEXT", "''", 1, 0, 0}, {"client", "TEXT", "''", 1, 0, 0}, {"contract_no", "TEXT", "''", 1, 0, 0}, {"amount", "REAL", "0", 1, 0, 0}, {"budget", "REAL", "0", 1, 0, 0}, {"plan_start", "TEXT", "''", 1, 0, 0}, {"plan_end", "TEXT", "''", 1, 0, 0}, {"remark", "TEXT", "''", 1, 0, 0}, {"close_reason", "TEXT", "''", 1, 0, 0}, {"status", "TEXT", "'active'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}},
	"sessions":                          {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"title", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'active'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}, {"pinned", "INTEGER", "0", 1, 0, 0}, {"revision", "INTEGER", "1", 1, 0, 0}},
	"messages":                          {{"id", "TEXT", "", 0, 1, 0}, {"session_id", "TEXT", "", 1, 0, 0}, {"role", "TEXT", "'user'", 1, 0, 0}, {"status", "TEXT", "'completed'", 1, 0, 0}, {"sequence", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"message_parts":                     {{"message_id", "TEXT", "", 1, 1, 0}, {"ordinal", "INTEGER", "", 1, 2, 0}, {"type", "TEXT", "'text'", 1, 0, 0}, {"text", "TEXT", "", 1, 0, 0}},
	"message_session_state":             {{"session_id", "TEXT", "", 0, 1, 0}, {"last_sequence", "INTEGER", "", 1, 0, 0}, {"message_count", "INTEGER", "", 1, 0, 0}, {"text_bytes", "INTEGER", "", 1, 0, 0}},
	"message_project_usage":             {{"project_id", "TEXT", "", 0, 1, 0}, {"text_bytes", "INTEGER", "", 1, 0, 0}},
	"message_workspace_usage":           {{"singleton", "INTEGER", "", 0, 1, 0}, {"text_bytes", "INTEGER", "", 1, 0, 0}},
	"token_ledger":                      {{"id", "TEXT", "", 0, 1, 0}, {"message_id", "TEXT", "", 1, 0, 0}, {"provider", "TEXT", "''", 1, 0, 0}, {"model", "TEXT", "''", 1, 0, 0}, {"tokenizer_revision", "TEXT", "''", 1, 0, 0}, {"token_count", "INTEGER", "", 1, 0, 0}, {"estimation_method", "TEXT", "", 1, 0, 0}, {"utf8_bytes", "INTEGER", "", 1, 0, 0}, {"computed_at", "TEXT", "", 1, 0, 0}, {"subject_type", "TEXT", "'message'", 1, 0, 0}, {"subject_id", "TEXT", "''", 1, 0, 0}, {"tokenizer_id", "TEXT", "'lunitide-canonical-v1'", 1, 0, 0}, {"invalidated_at", "TEXT", "", 0, 0, 0}},
	"compaction_checkpoints":            {{"id", "TEXT", "", 0, 1, 0}, {"session_id", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"source_start_id", "TEXT", "", 1, 0, 0}, {"source_end_id", "TEXT", "", 1, 0, 0}, {"source_start_seq", "INTEGER", "", 1, 0, 0}, {"source_end_seq", "INTEGER", "", 1, 0, 0}, {"source_digest", "TEXT", "", 1, 0, 0}, {"prev_checkpoint_id", "TEXT", "", 0, 0, 0}, {"prev_checkpoint_digest", "TEXT", "", 0, 0, 0}, {"summary_schema_version", "TEXT", "'1.0'", 1, 0, 0}, {"trigger", "TEXT", "", 1, 0, 0}, {"trigger_reason", "TEXT", "''", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"provider", "TEXT", "''", 1, 0, 0}, {"model", "TEXT", "''", 1, 0, 0}, {"summary_json", "TEXT", "'{}'", 1, 0, 0}, {"human_summary", "TEXT", "''", 1, 0, 0}, {"failure_code", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}},
	"compaction_activations":            {{"session_id", "TEXT", "", 0, 1, 0}, {"checkpoint_id", "TEXT", "", 0, 0, 0}, {"revision", "INTEGER", "0", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"compaction_activation_bases":       {{"checkpoint_id", "TEXT", "", 0, 1, 0}, {"base_revision", "INTEGER", "", 1, 0, 0}},
	"handoff_capsules":                  {{"id", "TEXT", "", 0, 1, 0}, {"source_session_id", "TEXT", "", 1, 0, 0}, {"dest_session_id", "TEXT", "", 0, 0, 0}, {"checkpoint_id", "TEXT", "", 1, 0, 0}, {"active_tasks_json", "TEXT", "'[]'", 1, 0, 0}, {"recent_message_ids", "TEXT", "'[]'", 1, 0, 0}, {"digest", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'active'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"activated_at", "TEXT", "", 0, 0, 0}, {"expires_at", "TEXT", "", 0, 0, 0}},
	"plans":                             {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"stage_id", "TEXT", "", 0, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"status", "TEXT", "'draft'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"plan_nodes":                        {{"id", "TEXT", "", 0, 1, 0}, {"plan_id", "TEXT", "", 1, 0, 0}, {"parent_node_id", "TEXT", "", 0, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"risk_level", "TEXT", "'low'", 1, 0, 0}, {"budget_tokens", "INTEGER", "", 0, 0, 0}, {"estimate_tokens", "INTEGER", "", 0, 0, 0}, {"worker_role", "TEXT", "''", 1, 0, 0}, {"sequence", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"governance_reviews":                {{"id", "TEXT", "", 0, 1, 0}, {"plan_id", "TEXT", "", 0, 0, 0}, {"node_id", "TEXT", "", 0, 0, 0}, {"action_type", "TEXT", "", 1, 0, 0}, {"action_digest", "TEXT", "", 1, 0, 0}, {"input_digest", "TEXT", "", 1, 0, 0}, {"state_digest", "TEXT", "", 1, 0, 0}, {"policy_version", "INTEGER", "", 1, 0, 0}, {"risk_level", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"reviewer_note", "TEXT", "''", 1, 0, 0}, {"expires_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"reviewed_at", "TEXT", "", 0, 0, 0}},
	"governance_policies":               {{"id", "TEXT", "", 0, 1, 0}, {"name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"is_active", "INTEGER", "1", 1, 0, 0}, {"rules_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"memories":                          {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"layer", "TEXT", "", 1, 0, 0}, {"scope", "TEXT", "", 1, 0, 0}, {"key", "TEXT", "", 1, 0, 0}, {"content", "TEXT", "", 1, 0, 0}, {"embedding_id", "TEXT", "", 0, 0, 0}, {"source_id", "TEXT", "", 0, 0, 0}, {"source_type", "TEXT", "", 0, 0, 0}, {"confidence", "REAL", "1.0", 1, 0, 0}, {"access_count", "INTEGER", "0", 1, 0, 0}, {"last_accessed", "TEXT", "", 0, 0, 0}, {"expires_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"ontology_nodes":                    {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"type", "TEXT", "", 1, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"full_path", "TEXT", "''", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"metadata_json", "TEXT", "'{}'", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"ontology_edges":                    {{"id", "TEXT", "", 0, 1, 0}, {"source_node_id", "TEXT", "", 1, 0, 0}, {"target_node_id", "TEXT", "", 1, 0, 0}, {"type", "TEXT", "", 1, 0, 0}, {"label", "TEXT", "''", 1, 0, 0}, {"properties_json", "TEXT", "'{}'", 1, 0, 0}, {"weight", "REAL", "1.0", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"skills":                            {{"id", "TEXT", "", 0, 1, 0}, {"name", "TEXT", "", 1, 0, 0}, {"display_name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"version", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'draft'", 1, 0, 0}, {"permissions_json", "TEXT", "", 1, 0, 0}, {"entry_point", "TEXT", "", 1, 0, 0}, {"manifest_json", "TEXT", "", 1, 0, 0}, {"signature", "TEXT", "", 0, 0, 0}, {"publisher_id", "TEXT", "", 0, 0, 0}, {"min_engine_version", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"stages":                            {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"phase", "INTEGER", "", 1, 0, 0}, {"title", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'not_started'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}},
	"plan_versions":                     {{"id", "TEXT", "", 0, 1, 0}, {"plan_id", "TEXT", "", 1, 0, 0}, {"version_no", "INTEGER", "", 1, 0, 0}, {"graph_hash", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"plan_edges":                        {{"id", "TEXT", "", 0, 1, 0}, {"plan_version_id", "TEXT", "", 1, 0, 0}, {"from_node_id", "TEXT", "", 1, 0, 0}, {"to_node_id", "TEXT", "", 1, 0, 0}, {"condition_json", "TEXT", "'{}'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"node_runs":                         {{"id", "TEXT", "", 0, 1, 0}, {"node_id", "TEXT", "", 1, 0, 0}, {"attempt", "INTEGER", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"result_ref", "TEXT", "", 0, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 0, 0, 0}, {"ended_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"node_run_checkpoints":              {{"id", "TEXT", "", 0, 1, 0}, {"node_run_id", "TEXT", "", 1, 0, 0}, {"state_ref", "TEXT", "", 1, 0, 0}, {"external_effect_digest", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"tool_calls":                        {{"id", "TEXT", "", 0, 1, 0}, {"node_run_id", "TEXT", "", 1, 0, 0}, {"tool_id", "TEXT", "", 1, 0, 0}, {"args_hash", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"result_ref", "TEXT", "", 0, 0, 0}, {"risk", "TEXT", "'low'", 1, 0, 0}, {"approval_id", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"approval_decisions":                {{"id", "TEXT", "", 0, 1, 0}, {"review_id", "TEXT", "", 1, 0, 0}, {"decision", "TEXT", "", 1, 0, 0}, {"actor", "TEXT", "", 1, 0, 0}, {"reason", "TEXT", "''", 1, 0, 0}, {"decided_at", "TEXT", "", 1, 0, 0}},
	"memory_sources":                    {{"id", "TEXT", "", 0, 1, 0}, {"memory_id", "TEXT", "", 1, 0, 0}, {"source_type", "TEXT", "", 1, 0, 0}, {"source_id", "TEXT", "", 1, 0, 0}, {"source_revision", "TEXT", "''", 1, 0, 0}, {"quote_ref", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"memory_revisions":                  {{"id", "TEXT", "", 0, 1, 0}, {"memory_id", "TEXT", "", 1, 0, 0}, {"old_ref", "TEXT", "", 0, 0, 0}, {"new_ref", "TEXT", "", 1, 0, 0}, {"reason", "TEXT", "''", 1, 0, 0}, {"actor", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"recall_events":                     {{"id", "TEXT", "", 0, 1, 0}, {"session_id", "TEXT", "", 1, 0, 0}, {"query_hash", "TEXT", "", 1, 0, 0}, {"memory_id", "TEXT", "", 1, 0, 0}, {"score", "REAL", "", 1, 0, 0}, {"rank", "INTEGER", "", 1, 0, 0}, {"injected_tokens", "INTEGER", "0", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"deletion_tombstones":               {{"owner_type", "TEXT", "", 1, 1, 0}, {"owner_id", "TEXT", "", 1, 2, 0}, {"deleted_at", "TEXT", "", 1, 0, 0}, {"propagation_status", "TEXT", "'pending'", 1, 0, 0}},
	"attachments":                       {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"session_id", "TEXT", "", 0, 0, 0}, {"file_ref", "TEXT", "", 1, 0, 0}, {"original_name", "TEXT", "", 1, 0, 0}, {"mime", "TEXT", "'application/octet-stream'", 1, 0, 0}, {"size", "INTEGER", "", 1, 0, 0}, {"sha256", "TEXT", "", 1, 0, 0}, {"parse_status", "TEXT", "'pending'", 1, 0, 0}, {"parse_error_code", "TEXT", "''", 1, 0, 0}, {"parsed_text", "TEXT", "''", 1, 0, 0}, {"parsed_text_bytes", "INTEGER", "0", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"deleted_at", "TEXT", "", 0, 0, 0}},
	"agent_plan_runs":                   {{"id", "TEXT", "", 0, 1, 0}, {"parent_run_id", "TEXT", "", 0, 0, 0}, {"plan_id", "TEXT", "", 1, 0, 0}, {"node_id", "TEXT", "", 1, 0, 0}, {"role", "TEXT", "", 1, 0, 0}, {"todo_id", "TEXT", "", 1, 0, 0}, {"todo_title", "TEXT", "", 1, 0, 0}, {"todo_description", "TEXT", "''", 1, 0, 0}, {"todo_metadata_json", "TEXT", "'{}'", 1, 0, 0}, {"status", "TEXT", "", 1, 0, 0}, {"depth", "INTEGER", "", 1, 0, 0}, {"failure", "TEXT", "''", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"terminal_at", "TEXT", "", 0, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}},
	"agent_plan_run_events":             {{"sequence", "INTEGER", "", 0, 1, 0}, {"run_id", "TEXT", "", 1, 0, 0}, {"type", "TEXT", "", 1, 0, 0}, {"from_status", "TEXT", "''", 1, 0, 0}, {"to_status", "TEXT", "", 1, 0, 0}, {"detail", "TEXT", "''", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
}

func validateSchema(ctx context.Context, q sqlRunner) (int64, string, error) {
	var integrity string
	if err := q.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return 0, "", fmt.Errorf("integrity check: %q: %w", integrity, err)
	}
	fkRows, err := q.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return 0, "", fmt.Errorf("foreign key check: %w", err)
	}
	if fkRows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var fkID int
		if err = fkRows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			fkRows.Close()
			return 0, "", fmt.Errorf("foreign key check: %w", err)
		}
		fkRows.Close()
		return 0, "", fmt.Errorf("foreign key violation: table=%s rowid=%v parent=%s constraint=%d", table, rowID, parent, fkID)
	}
	if err = fkRows.Err(); err != nil {
		fkRows.Close()
		return 0, "", fmt.Errorf("foreign key check: %w", err)
	}
	if err = fkRows.Close(); err != nil {
		return 0, "", err
	}
	rows, err := q.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var canonical []string
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			return 0, "", err
		}
		key := typ + ":" + name
		want, ok := expectedSchemaSQL[key]
		if !ok {
			return 0, "", fmt.Errorf("unknown schema object %s", key)
		}
		if sqlText != want {
			return 0, "", fmt.Errorf("schema definition mismatch for %s: got %q want %q", key, sqlText, want)
		}
		seen[key] = true
		canonical = append(canonical, key+":"+sqlText)
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	if len(seen) != len(expectedSchemaSQL) {
		return 0, "", fmt.Errorf("schema definition object set incomplete")
	}
	for _, table := range []string{"providers", "provider_models", "projects", "sessions", "messages", "message_parts", "token_ledger", "compaction_checkpoints", "compaction_activations", "compaction_activation_bases", "handoff_capsules", "schema_migrations", "provider_tests", "idempotency_records", "idempotency_claims", "outbox_events", "audit_events", "credential_adoptions", "provider_metadata_migrations", "provider_metadata_migration_items", "plans", "plan_nodes", "governance_reviews", "governance_policies", "memories", "ontology_nodes", "ontology_edges", "skills", "stages", "plan_versions", "plan_edges", "node_runs", "node_run_checkpoints", "tool_calls", "approval_decisions", "memory_sources", "memory_revisions", "recall_events", "deletion_tombstones"} {
		r, e := q.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_xinfo('%s')`, table))
		if e != nil {
			return 0, "", e
		}
		var got []columnSpec
		for r.Next() {
			var cid int
			var c columnSpec
			var def sql.NullString
			if e = r.Scan(&cid, &c.name, &c.kind, &c.notNull, &def, &c.pk, &c.hidden); e != nil {
				r.Close()
				return 0, "", e
			}
			c.def = def.String
			got = append(got, c)
		}
		r.Close()
		want := expectedColumns[table]
		if len(got) != len(want) {
			return 0, "", fmt.Errorf("column set mismatch for %s", table)
		}
		for i := range want {
			if got[i] != want[i] {
				return 0, "", fmt.Errorf("column definition mismatch for %s.%s", table, want[i].name)
			}
			canonical = append(canonical, fmt.Sprintf("column:%s:%#v", table, got[i]))
		}
	}
	var table, from, to, update, del, match string
	var id, seq int
	if err := q.QueryRowContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('provider_models')`).Scan(&id, &seq, &table, &from, &to, &update, &del, &match); err != nil || id != 0 || seq != 0 || table != "providers" || from != "provider_id" || to != "id" || update != "NO ACTION" || del != "CASCADE" || match != "NONE" {
		return 0, "", fmt.Errorf("foreign key mismatch: %w", err)
	}
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('provider_models')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("foreign key set mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("fk:%d:%d:%s:%s:%s:%s:%s:%s", id, seq, table, from, to, update, del, match))
	if err := q.QueryRowContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('sessions')`).Scan(&id, &seq, &table, &from, &to, &update, &del, &match); err != nil || id != 0 || seq != 0 || table != "projects" || from != "project_id" || to != "id" || update != "NO ACTION" || del != "RESTRICT" || match != "NONE" {
		return 0, "", fmt.Errorf("sessions foreign key mismatch: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('sessions')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("sessions foreign key set mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("fk:sessions:%d:%d:%s:%s:%s:%s:%s:%s", id, seq, table, from, to, update, del, match))
	// stages FK: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('stages')`).Scan(&id, &seq, &table, &from, &to, &update, &del, &match); err != nil || id != 0 || seq != 0 || table != "projects" || from != "project_id" || to != "id" || update != "NO ACTION" || del != "RESTRICT" || match != "NONE" {
		return 0, "", fmt.Errorf("stages foreign key mismatch: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('stages')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("stages foreign key set mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("fk:stages:%d:%d:%s:%s:%s:%s:%s:%s", id, seq, table, from, to, update, del, match))
	// compaction_checkpoints FKs: session_id, source_start_id, source_end_id, prev_checkpoint_id
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('compaction_checkpoints')`).Scan(&count); err != nil || count != 4 {
		return 0, "", fmt.Errorf("compaction_checkpoints FK set mismatch: count=%d err=%w", count, err)
	}
	var cpFkRows *sql.Rows
	cpFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('compaction_checkpoints') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("compaction_checkpoints FK query: %w", err)
	}
	type fkRow struct {
		id, seq                             int
		table, from, to, update, del, match string
	}
	cpFKs := map[string]fkRow{}
	for cpFkRows.Next() {
		var r fkRow
		if err := cpFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			cpFkRows.Close()
			return 0, "", err
		}
		cpFKs[r.from] = r
	}
	cpFkRows.Close()
	if len(cpFKs) != 4 {
		return 0, "", fmt.Errorf("compaction_checkpoints FK count mismatch: got %d", len(cpFKs))
	}
	// FK: session_id → sessions
	if r, ok := cpFKs["session_id"]; !ok || r.seq != 0 || r.table != "sessions" || r.from != "session_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK session_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:session_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["session_id"].id, cpFKs["session_id"].seq, cpFKs["session_id"].table, cpFKs["session_id"].from, cpFKs["session_id"].to, cpFKs["session_id"].update, cpFKs["session_id"].del, cpFKs["session_id"].match))
	// FK: source_start_id → messages
	if r, ok := cpFKs["source_start_id"]; !ok || r.seq != 0 || r.table != "messages" || r.from != "source_start_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK source_start_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:source_start_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["source_start_id"].id, cpFKs["source_start_id"].seq, cpFKs["source_start_id"].table, cpFKs["source_start_id"].from, cpFKs["source_start_id"].to, cpFKs["source_start_id"].update, cpFKs["source_start_id"].del, cpFKs["source_start_id"].match))
	// FK: source_end_id → messages
	if r, ok := cpFKs["source_end_id"]; !ok || r.seq != 0 || r.table != "messages" || r.from != "source_end_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK source_end_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:source_end_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["source_end_id"].id, cpFKs["source_end_id"].seq, cpFKs["source_end_id"].table, cpFKs["source_end_id"].from, cpFKs["source_end_id"].to, cpFKs["source_end_id"].update, cpFKs["source_end_id"].del, cpFKs["source_end_id"].match))
	// FK: prev_checkpoint_id → compaction_checkpoints
	if r, ok := cpFKs["prev_checkpoint_id"]; !ok || r.seq != 0 || r.table != "compaction_checkpoints" || r.from != "prev_checkpoint_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK prev_checkpoint_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:prev_checkpoint_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["prev_checkpoint_id"].id, cpFKs["prev_checkpoint_id"].seq, cpFKs["prev_checkpoint_id"].table, cpFKs["prev_checkpoint_id"].from, cpFKs["prev_checkpoint_id"].to, cpFKs["prev_checkpoint_id"].update, cpFKs["prev_checkpoint_id"].del, cpFKs["prev_checkpoint_id"].match))
	// handoff_capsules FKs: source_session_id, dest_session_id, checkpoint_id
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('handoff_capsules')`).Scan(&count); err != nil || count != 3 {
		return 0, "", fmt.Errorf("handoff_capsules FK set mismatch: count=%d err=%w", count, err)
	}
	var hcFkRows *sql.Rows
	hcFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('handoff_capsules') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("handoff_capsules FK query: %w", err)
	}
	hcFKs := map[string]fkRow{}
	for hcFkRows.Next() {
		var r fkRow
		if err := hcFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			hcFkRows.Close()
			return 0, "", err
		}
		hcFKs[r.from] = r
	}
	hcFkRows.Close()
	if len(hcFKs) != 3 {
		return 0, "", fmt.Errorf("handoff_capsules FK count mismatch: got %d", len(hcFKs))
	}
	// FK: source_session_id → sessions
	if r, ok := hcFKs["source_session_id"]; !ok || r.seq != 0 || r.table != "sessions" || r.from != "source_session_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("handoff_capsules FK source_session_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:handoff_capsules:source_session_id:%d:%d:%s:%s:%s:%s:%s:%s", hcFKs["source_session_id"].id, hcFKs["source_session_id"].seq, hcFKs["source_session_id"].table, hcFKs["source_session_id"].from, hcFKs["source_session_id"].to, hcFKs["source_session_id"].update, hcFKs["source_session_id"].del, hcFKs["source_session_id"].match))
	// FK: dest_session_id → sessions
	if r, ok := hcFKs["dest_session_id"]; !ok || r.seq != 0 || r.table != "sessions" || r.from != "dest_session_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "SET NULL" || r.match != "NONE" {
		return 0, "", fmt.Errorf("handoff_capsules FK dest_session_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:handoff_capsules:dest_session_id:%d:%d:%s:%s:%s:%s:%s:%s", hcFKs["dest_session_id"].id, hcFKs["dest_session_id"].seq, hcFKs["dest_session_id"].table, hcFKs["dest_session_id"].from, hcFKs["dest_session_id"].to, hcFKs["dest_session_id"].update, hcFKs["dest_session_id"].del, hcFKs["dest_session_id"].match))
	// FK: checkpoint_id → compaction_checkpoints
	if r, ok := hcFKs["checkpoint_id"]; !ok || r.seq != 0 || r.table != "compaction_checkpoints" || r.from != "checkpoint_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("handoff_capsules FK checkpoint_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:handoff_capsules:checkpoint_id:%d:%d:%s:%s:%s:%s:%s:%s", hcFKs["checkpoint_id"].id, hcFKs["checkpoint_id"].seq, hcFKs["checkpoint_id"].table, hcFKs["checkpoint_id"].from, hcFKs["checkpoint_id"].to, hcFKs["checkpoint_id"].update, hcFKs["checkpoint_id"].del, hcFKs["checkpoint_id"].match))
	// plans FKs: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('plans')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("plans FK set mismatch: count=%d err=%w", count, err)
	}
	var plansFkRows *sql.Rows
	plansFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('plans') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("plans FK query: %w", err)
	}
	plansFKs := map[string]fkRow{}
	for plansFkRows.Next() {
		var r fkRow
		if err := plansFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			plansFkRows.Close()
			return 0, "", err
		}
		plansFKs[r.from] = r
	}
	plansFkRows.Close()
	if len(plansFKs) != 1 {
		return 0, "", fmt.Errorf("plans FK count mismatch: got %d", len(plansFKs))
	}
	// FK: project_id → projects
	if r, ok := plansFKs["project_id"]; !ok || r.seq != 0 || r.table != "projects" || r.from != "project_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("plans FK project_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:plans:project_id:%d:%d:%s:%s:%s:%s:%s:%s", plansFKs["project_id"].id, plansFKs["project_id"].seq, plansFKs["project_id"].table, plansFKs["project_id"].from, plansFKs["project_id"].to, plansFKs["project_id"].update, plansFKs["project_id"].del, plansFKs["project_id"].match))
	// plan_nodes FKs: plan_id → plans, parent_node_id → plan_nodes
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('plan_nodes')`).Scan(&count); err != nil || count != 2 {
		return 0, "", fmt.Errorf("plan_nodes FK set mismatch: count=%d err=%w", count, err)
	}
	var pnFkRows *sql.Rows
	pnFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('plan_nodes') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("plan_nodes FK query: %w", err)
	}
	pnFKs := map[string]fkRow{}
	for pnFkRows.Next() {
		var r fkRow
		if err := pnFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			pnFkRows.Close()
			return 0, "", err
		}
		pnFKs[r.from] = r
	}
	pnFkRows.Close()
	if len(pnFKs) != 2 {
		return 0, "", fmt.Errorf("plan_nodes FK count mismatch: got %d", len(pnFKs))
	}
	// FK: plan_id → plans
	if r, ok := pnFKs["plan_id"]; !ok || r.seq != 0 || r.table != "plans" || r.from != "plan_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "CASCADE" || r.match != "NONE" {
		return 0, "", fmt.Errorf("plan_nodes FK plan_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:plan_nodes:plan_id:%d:%d:%s:%s:%s:%s:%s:%s", pnFKs["plan_id"].id, pnFKs["plan_id"].seq, pnFKs["plan_id"].table, pnFKs["plan_id"].from, pnFKs["plan_id"].to, pnFKs["plan_id"].update, pnFKs["plan_id"].del, pnFKs["plan_id"].match))
	// FK: parent_node_id → plan_nodes
	if r, ok := pnFKs["parent_node_id"]; !ok || r.seq != 0 || r.table != "plan_nodes" || r.from != "parent_node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("plan_nodes FK parent_node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:plan_nodes:parent_node_id:%d:%d:%s:%s:%s:%s:%s:%s", pnFKs["parent_node_id"].id, pnFKs["parent_node_id"].seq, pnFKs["parent_node_id"].table, pnFKs["parent_node_id"].from, pnFKs["parent_node_id"].to, pnFKs["parent_node_id"].update, pnFKs["parent_node_id"].del, pnFKs["parent_node_id"].match))
	// governance_reviews FKs: plan_id → plans, node_id → plan_nodes
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('governance_reviews')`).Scan(&count); err != nil || count != 2 {
		return 0, "", fmt.Errorf("governance_reviews FK set mismatch: count=%d err=%w", count, err)
	}
	var grFkRows *sql.Rows
	grFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('governance_reviews') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("governance_reviews FK query: %w", err)
	}
	grFKs := map[string]fkRow{}
	for grFkRows.Next() {
		var r fkRow
		if err := grFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			grFkRows.Close()
			return 0, "", err
		}
		grFKs[r.from] = r
	}
	grFkRows.Close()
	if len(grFKs) != 2 {
		return 0, "", fmt.Errorf("governance_reviews FK count mismatch: got %d", len(grFKs))
	}
	// FK: plan_id → plans
	if r, ok := grFKs["plan_id"]; !ok || r.seq != 0 || r.table != "plans" || r.from != "plan_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("governance_reviews FK plan_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:governance_reviews:plan_id:%d:%d:%s:%s:%s:%s:%s:%s", grFKs["plan_id"].id, grFKs["plan_id"].seq, grFKs["plan_id"].table, grFKs["plan_id"].from, grFKs["plan_id"].to, grFKs["plan_id"].update, grFKs["plan_id"].del, grFKs["plan_id"].match))
	// FK: node_id → plan_nodes
	if r, ok := grFKs["node_id"]; !ok || r.seq != 0 || r.table != "plan_nodes" || r.from != "node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("governance_reviews FK node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:governance_reviews:node_id:%d:%d:%s:%s:%s:%s:%s:%s", grFKs["node_id"].id, grFKs["node_id"].seq, grFKs["node_id"].table, grFKs["node_id"].from, grFKs["node_id"].to, grFKs["node_id"].update, grFKs["node_id"].del, grFKs["node_id"].match))
	// memories FKs: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('memories')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("memories FK set mismatch: count=%d err=%w", count, err)
	}
	var memFkRows *sql.Rows
	memFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('memories') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("memories FK query: %w", err)
	}
	memFKs := map[string]fkRow{}
	for memFkRows.Next() {
		var r fkRow
		if err := memFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			memFkRows.Close()
			return 0, "", err
		}
		memFKs[r.from] = r
	}
	memFkRows.Close()
	if len(memFKs) != 1 {
		return 0, "", fmt.Errorf("memories FK count mismatch: got %d", len(memFKs))
	}
	// FK: project_id → projects
	if r, ok := memFKs["project_id"]; !ok || r.seq != 0 || r.table != "projects" || r.from != "project_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("memories FK project_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:memories:project_id:%d:%d:%s:%s:%s:%s:%s:%s", memFKs["project_id"].id, memFKs["project_id"].seq, memFKs["project_id"].table, memFKs["project_id"].from, memFKs["project_id"].to, memFKs["project_id"].update, memFKs["project_id"].del, memFKs["project_id"].match))
	// ontology_nodes FKs: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('ontology_nodes')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("ontology_nodes FK set mismatch: count=%d err=%w", count, err)
	}
	var onFkRows *sql.Rows
	onFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('ontology_nodes') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("ontology_nodes FK query: %w", err)
	}
	onFKs := map[string]fkRow{}
	for onFkRows.Next() {
		var r fkRow
		if err := onFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			onFkRows.Close()
			return 0, "", err
		}
		onFKs[r.from] = r
	}
	onFkRows.Close()
	if len(onFKs) != 1 {
		return 0, "", fmt.Errorf("ontology_nodes FK count mismatch: got %d", len(onFKs))
	}
	// FK: project_id → projects
	if r, ok := onFKs["project_id"]; !ok || r.seq != 0 || r.table != "projects" || r.from != "project_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("ontology_nodes FK project_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:ontology_nodes:project_id:%d:%d:%s:%s:%s:%s:%s:%s", onFKs["project_id"].id, onFKs["project_id"].seq, onFKs["project_id"].table, onFKs["project_id"].from, onFKs["project_id"].to, onFKs["project_id"].update, onFKs["project_id"].del, onFKs["project_id"].match))
	// ontology_edges FKs: source_node_id → ontology_nodes, target_node_id → ontology_nodes
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('ontology_edges')`).Scan(&count); err != nil || count != 2 {
		return 0, "", fmt.Errorf("ontology_edges FK set mismatch: count=%d err=%w", count, err)
	}
	var oeFkRows *sql.Rows
	oeFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('ontology_edges') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("ontology_edges FK query: %w", err)
	}
	oeFKs := map[string]fkRow{}
	for oeFkRows.Next() {
		var r fkRow
		if err := oeFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			oeFkRows.Close()
			return 0, "", err
		}
		oeFKs[r.from] = r
	}
	oeFkRows.Close()
	if len(oeFKs) != 2 {
		return 0, "", fmt.Errorf("ontology_edges FK count mismatch: got %d", len(oeFKs))
	}
	// FK: source_node_id → ontology_nodes
	if r, ok := oeFKs["source_node_id"]; !ok || r.seq != 0 || r.table != "ontology_nodes" || r.from != "source_node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "CASCADE" || r.match != "NONE" {
		return 0, "", fmt.Errorf("ontology_edges FK source_node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:ontology_edges:source_node_id:%d:%d:%s:%s:%s:%s:%s:%s", oeFKs["source_node_id"].id, oeFKs["source_node_id"].seq, oeFKs["source_node_id"].table, oeFKs["source_node_id"].from, oeFKs["source_node_id"].to, oeFKs["source_node_id"].update, oeFKs["source_node_id"].del, oeFKs["source_node_id"].match))
	// FK: target_node_id → ontology_nodes
	if r, ok := oeFKs["target_node_id"]; !ok || r.seq != 0 || r.table != "ontology_nodes" || r.from != "target_node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "CASCADE" || r.match != "NONE" {
		return 0, "", fmt.Errorf("ontology_edges FK target_node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:ontology_edges:target_node_id:%d:%d:%s:%s:%s:%s:%s:%s", oeFKs["target_node_id"].id, oeFKs["target_node_id"].seq, oeFKs["target_node_id"].table, oeFKs["target_node_id"].from, oeFKs["target_node_id"].to, oeFKs["target_node_id"].update, oeFKs["target_node_id"].del, oeFKs["target_node_id"].match))
	// Extended entity tables (0018): validate FK sets and append canonical entries.
	extendedFKExpectations := []struct {
		table string
		count int
		fks   map[string]fkRow
	}{
		{"plan_versions", 1, map[string]fkRow{"plan_id": {0, 0, "plans", "plan_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"plan_edges", 3, map[string]fkRow{
			"plan_version_id": {0, 0, "plan_versions", "plan_version_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"from_node_id":    {1, 0, "plan_nodes", "from_node_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"to_node_id":      {2, 0, "plan_nodes", "to_node_id", "id", "NO ACTION", "CASCADE", "NONE"},
		}},
		{"node_runs", 1, map[string]fkRow{"node_id": {0, 0, "plan_nodes", "node_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"node_run_checkpoints", 1, map[string]fkRow{"node_run_id": {0, 0, "node_runs", "node_run_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"tool_calls", 2, map[string]fkRow{
			"node_run_id": {0, 0, "node_runs", "node_run_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"approval_id": {1, 0, "governance_reviews", "approval_id", "id", "NO ACTION", "NO ACTION", "NONE"},
		}},
		{"approval_decisions", 1, map[string]fkRow{"review_id": {0, 0, "governance_reviews", "review_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"memory_sources", 1, map[string]fkRow{"memory_id": {0, 0, "memories", "memory_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"memory_revisions", 1, map[string]fkRow{"memory_id": {0, 0, "memories", "memory_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"recall_events", 2, map[string]fkRow{
			"session_id": {0, 0, "sessions", "session_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"memory_id":  {1, 0, "memories", "memory_id", "id", "NO ACTION", "CASCADE", "NONE"},
		}},
	}
	for _, exp := range extendedFKExpectations {
		if err := q.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM pragma_foreign_key_list('%s')`, exp.table)).Scan(&count); err != nil || count != exp.count {
			return 0, "", fmt.Errorf("%s FK set mismatch: count=%d err=%w", exp.table, count, err)
		}
		ekFkRows, e := q.QueryContext(ctx, fmt.Sprintf(`SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('%s') ORDER BY id`, exp.table))
		if e != nil {
			return 0, "", fmt.Errorf("%s FK query: %w", exp.table, e)
		}
		gotFKs := map[string]fkRow{}
		for ekFkRows.Next() {
			var r fkRow
			if e = ekFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); e != nil {
				ekFkRows.Close()
				return 0, "", e
			}
			gotFKs[r.from] = r
		}
		ekFkRows.Close()
		if len(gotFKs) != exp.count {
			return 0, "", fmt.Errorf("%s FK count mismatch: got %d", exp.table, len(gotFKs))
		}
		// Iterate FK columns in deterministic id order to keep the canonical
		// fingerprint stable across runs (Go map iteration is randomized).
		orderedCols := make([]string, 0, len(exp.fks))
		for col := range exp.fks {
			orderedCols = append(orderedCols, col)
		}
		sort.Strings(orderedCols)
		for _, from := range orderedCols {
			want := exp.fks[from]
			r, ok := gotFKs[from]
			if !ok || r.seq != 0 || r.table != want.table || r.from != from || r.to != want.to || r.update != want.update || r.del != want.del || r.match != want.match {
				return 0, "", fmt.Errorf("%s FK %s mismatch: %+v", exp.table, from, r)
			}
			canonical = append(canonical, fmt.Sprintf("fk:%s:%s:%d:%d:%s:%s:%s:%s:%s:%s", exp.table, from, r.id, r.seq, r.table, r.from, r.to, r.update, r.del, r.match))
		}
	}
	var unique, partial int
	var origin, columns string
	if err := q.QueryRowContext(ctx, `SELECT "unique",origin,partial FROM pragma_index_list('provider_models') WHERE name='ux_provider_default_model'`).Scan(&unique, &origin, &partial); err != nil || unique != 1 || origin != "c" || partial != 1 {
		return 0, "", fmt.Errorf("index mismatch: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT group_concat(name, ',') FROM pragma_index_info('ux_provider_default_model')`).Scan(&columns); err != nil || columns != "provider_id" {
		return 0, "", fmt.Errorf("index columns mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("index:%d:%s:%d:%s", unique, origin, partial, columns))
	var version int64
	if err := q.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&version); err != nil {
		return 0, "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(canonical, "\n")))
	return version, hex.EncodeToString(sum[:]), nil
}

func (s *Store) ListProjects(ctx context.Context, filter project.Filter) ([]project.Project, error) {
	query := `SELECT id,name,project_code,project_type,description,summary,objective,client,contract_no,amount,budget,plan_start,plan_end,remark,close_reason,status,created_at,updated_at,version FROM projects`
	args := []any{}
	conditions := []string{}
	if filter.Status != "" {
		conditions = append(conditions, `status=?`)
		args = append(args, filter.Status)
	}
	if filter.Type != "" {
		conditions = append(conditions, `project_type=?`)
		args = append(args, filter.Type)
	}
	if len(conditions) > 0 {
		query += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	query += ` ORDER BY created_at,id LIMIT 101`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []project.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, errors.New("project data invariant violation: list exceeds capacity")
	}
	return items, nil
}

func scanProject(rows *sql.Rows) (project.Project, error) {
	var p project.Project
	var created, updated string
	if err := rows.Scan(&p.ID, &p.Name, &p.ProjectCode, &p.Type, &p.Description, &p.Summary, &p.Objective, &p.Client, &p.ContractNo, &p.Amount, &p.Budget, &p.PlanStart, &p.PlanEnd, &p.Remark, &p.CloseReason, &p.Status, &created, &updated, &p.Version); err != nil {
		return p, err
	}
	var err error
	if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return p, err
	}
	if p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return p, err
	}
	return p, p.Validate()
}

func (s *Store) ListSessions(ctx context.Context, filter session.Filter) ([]session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,title,pinned,status,created_at,updated_at,revision FROM sessions WHERE project_id=? ORDER BY pinned DESC,created_at,id LIMIT 101`, filter.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []session.Session{}
	for rows.Next() {
		var v session.Session
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Pinned, &v.Status, &created, &updated, &v.Version); err != nil {
			return nil, err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		if err = v.Validate(); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, errors.New("session data invariant violation: list exceeds capacity")
	}
	return items, nil
}

func (s *Store) ListMessages(ctx context.Context, q messageapp.PageQuery) ([]message.Message, int64, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, false, err
	}
	defer tx.Rollback()
	var snapshot int64
	if err := tx.QueryRowContext(ctx, `SELECT last_sequence FROM message_session_state WHERE session_id=?`, q.SessionID).Scan(&snapshot); err != nil {
		if err == sql.ErrNoRows {
			return nil, 0, false, messageapp.ErrSessionNotFound
		}
		return nil, 0, false, err
	}
	if q.Snapshot != 0 {
		snapshot = q.Snapshot
	}
	boundary := q.Boundary
	if boundary == 0 && q.Direction == messageapp.Backward {
		boundary = snapshot + 1
	}
	op, order := ">", "ASC"
	if q.Direction == messageapp.Backward {
		op, order = "<", "DESC"
	}
	statement := fmt.Sprintf(`SELECT m.id,m.session_id,m.role,m.status,m.sequence,MAX(CASE WHEN p.ordinal=1 AND p.type='text' THEN p.text END),m.created_at,count(p.message_id),count(CASE WHEN p.ordinal=1 AND p.type='text' THEN 1 END) FROM messages m LEFT JOIN message_parts p ON p.message_id=m.id WHERE m.session_id=? AND m.sequence<=? AND m.sequence %s ? GROUP BY m.id ORDER BY m.sequence %s LIMIT ?`, op, order)
	rows, err := tx.QueryContext(ctx, statement, q.SessionID, snapshot, boundary, q.Limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	items := make([]message.Message, 0, q.Limit+1)
	for rows.Next() {
		var v message.Message
		var created string
		var text sql.NullString
		var parts, validParts int
		if err = rows.Scan(&v.ID, &v.SessionID, &v.Role, &v.Status, &v.Sequence, &text, &created, &parts, &validParts); err != nil {
			return nil, 0, false, err
		}
		if parts != 1 || validParts != 1 || !text.Valid {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		v.Text = text.String
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		expected := boundary + int64(len(items)) + 1
		if q.Direction == messageapp.Backward {
			expected = boundary - int64(len(items)) - 1
		}
		if err != nil || v.Validate() != nil || v.Sequence != expected {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	more := len(items) > q.Limit
	if more {
		items = items[:q.Limit]
	}
	if !more && snapshot > 0 {
		if q.Direction == messageapp.Forward && (len(items) == 0 || items[len(items)-1].Sequence != snapshot) {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		if q.Direction == messageapp.Backward && (len(items) == 0 || items[len(items)-1].Sequence != 1) {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
	}
	if err = rows.Close(); err != nil {
		return nil, 0, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, false, err
	}
	return items, snapshot, more, nil
}

func (s *Store) List(ctx context.Context, filter provider.Filter) ([]provider.Provider, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := listProvidersWith(ctx, tx, filter)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func listProvidersWith(ctx context.Context, q sqlRunner, filter provider.Filter) ([]provider.Provider, error) {
	query := `SELECT id, COALESCE(legacy_id,''), name, protocol, base_url, COALESCE(credential_ref,''), credential_state, status, created_at, updated_at, version FROM providers WHERE deleted_at IS NULL`
	args := []any{}
	if filter.Protocol != "" {
		query += ` AND protocol = ?`
		args = append(args, filter.Protocol)
	}
	query += ` ORDER BY created_at, id`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()
	result := []provider.Provider{}
	for rows.Next() {
		var item provider.Provider
		var created, updated string
		if err := rows.Scan(&item.ID, &item.LegacyID, &item.Name, &item.Protocol, &item.BaseURL, &item.CredentialRef, &item.CredentialState, &item.Status, &created, &updated, &item.Version); err != nil {
			return nil, err
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Models, err = listModelsWith(ctx, q, result[i].ID)
		if err != nil {
			return nil, err
		}
		if err = result[i].Validate(); err != nil {
			return nil, err
		}
	}
	return result, rows.Err()
}

func (s *Store) Create(ctx context.Context, item provider.Provider) (provider.Provider, error) {
	origin, err := provider.NormalizeBaseURL(item.BaseURL)
	if err != nil {
		return provider.Provider{}, err
	}
	item.BaseURL = origin
	if item.ID == "" {
		item.ID, err = s.newULID(time.Now())
		if err != nil {
			return provider.Provider{}, err
		}
	} else if parsed, parseErr := ulid.ParseStrict(item.ID); parseErr != nil || parsed.String() != item.ID {
		return provider.Provider{}, fmt.Errorf("provider ID must be an uppercase canonical ULID")
	}
	if item.Status == "" {
		item.Status = provider.StatusEnabled
	}
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt, item.Version = now, now, 1
	if err := item.Validate(); err != nil {
		return provider.Provider{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return provider.Provider{}, err
	}
	defer tx.Rollback()
	fingerprint, err := provider.OriginFingerprint(item.Protocol, item.BaseURL)
	if err != nil {
		return provider.Provider{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,origin_fingerprint) VALUES(?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?)`, item.ID, nullString(item.LegacyID), item.Name, item.Protocol, item.BaseURL, item.CredentialRef, item.CredentialState, item.Status, formatTime(item.CreatedAt), formatTime(item.UpdatedAt), item.Version, fingerprint)
	if err != nil {
		return provider.Provider{}, fmt.Errorf("create provider: %w", err)
	}
	if err = replaceModels(ctx, tx, item.ID, item.Models); err != nil {
		return provider.Provider{}, err
	}
	if err = tx.Commit(); err != nil {
		return provider.Provider{}, err
	}
	return item, nil
}

func (s *Store) Get(ctx context.Context, id string) (provider.Provider, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return provider.Provider{}, err
	}
	defer tx.Rollback()
	item, err := getProvider(ctx, tx, id)
	if err != nil {
		return provider.Provider{}, err
	}
	if err = tx.Commit(); err != nil {
		return provider.Provider{}, err
	}
	return item, nil
}

func (s *Store) Update(ctx context.Context, item provider.Provider, expectedVersion int64) (provider.Provider, error) {
	origin, err := provider.NormalizeBaseURL(item.BaseURL)
	if err != nil {
		return provider.Provider{}, err
	}
	item.BaseURL = origin
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	old, err := getProvider(ctx, conn, item.ID)
	if err != nil {
		return provider.Provider{}, err
	}
	if old.Version != expectedVersion {
		return provider.Provider{}, provider.ErrConflict
	}
	var oldFingerprint string
	if err = conn.QueryRowContext(ctx, `SELECT origin_fingerprint FROM providers WHERE id=?`, item.ID).Scan(&oldFingerprint); err != nil {
		return provider.Provider{}, err
	}
	wantOld, _ := provider.OriginFingerprint(old.Protocol, old.BaseURL)
	if oldFingerprint != wantOld {
		return provider.Provider{}, fmt.Errorf("provider origin fingerprint mismatch")
	}
	newFingerprint, _ := provider.OriginFingerprint(item.Protocol, item.BaseURL)
	if oldFingerprint != newFingerprint {
		if item.CredentialRef == old.CredentialRef && old.CredentialRef != "" {
			return provider.Provider{}, provider.ErrCredentialReentryRequired
		}
		if item.CredentialRef == "" && item.CredentialState != provider.CredentialRequiresReentry {
			return provider.Provider{}, provider.ErrCredentialReentryRequired
		}
	}
	item.CreatedAt = old.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	item.Version = expectedVersion + 1
	if err = item.Validate(); err != nil {
		return provider.Provider{}, err
	}
	r, err := conn.ExecContext(ctx, `UPDATE providers SET legacy_id=?,name=?,protocol=?,base_url=?,credential_ref=NULLIF(?,''),credential_state=?,status=?,updated_at=?,version=?,origin_fingerprint=? WHERE id=? AND version=? AND deleted_at IS NULL`, nullString(item.LegacyID), item.Name, item.Protocol, item.BaseURL, item.CredentialRef, item.CredentialState, item.Status, formatTime(item.UpdatedAt), item.Version, newFingerprint, item.ID, expectedVersion)
	if err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return provider.Provider{}, provider.ErrConflict
	}
	if err = replaceModels(ctx, conn, item.ID, item.Models); err != nil {
		return provider.Provider{}, err
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	return item, nil
}

// Delete soft-deletes a live provider. ErrNotFound for an already deleted ID
// lets the service layer deliberately choose strict or idempotent semantics.
func (s *Store) Delete(ctx context.Context, id string, expectedVersion int64) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return mapWriteError(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return mapWriteError(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	now := formatTime(time.Now().UTC())
	r, err := conn.ExecContext(ctx, `UPDATE providers SET deleted_at=?,updated_at=?,version=version+1 WHERE id=? AND version=? AND deleted_at IS NULL`, now, now, id, expectedVersion)
	if err != nil {
		return mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n == 1 {
		_, err = conn.ExecContext(ctx, `COMMIT`)
		return mapWriteError(err)
	}
	var live int
	err = conn.QueryRowContext(ctx, `SELECT count(*) FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&live)
	if err != nil {
		return err
	}
	if live == 1 {
		return provider.ErrConflict
	}
	return provider.ErrNotFound
}

func getProvider(ctx context.Context, q sqlRunner, id string) (provider.Provider, error) {
	var item provider.Provider
	var created, updated string
	err := q.QueryRowContext(ctx, `SELECT id,COALESCE(legacy_id,''),name,protocol,base_url,COALESCE(credential_ref,''),credential_state,status,created_at,updated_at,version FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&item.ID, &item.LegacyID, &item.Name, &item.Protocol, &item.BaseURL, &item.CredentialRef, &item.CredentialState, &item.Status, &created, &updated, &item.Version)
	if err == sql.ErrNoRows {
		return item, provider.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	if err == nil {
		item.Models, err = listModelsWith(ctx, q, id)
	}
	return item, err
}

func replaceModels(ctx context.Context, tx sqlRunner, id string, models []provider.Model) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM provider_models WHERE provider_id=?`, id); err != nil {
		return err
	}
	for position, model := range models {
		var cw any
		if model.ContextWindow > 0 {
			cw = model.ContextWindow
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position,context_window) VALUES(?,?,?,?,?,?)`, id, model.ModelID, model.DisplayName, model.IsDefault, position, cw); err != nil {
			return fmt.Errorf("write provider models: %w", err)
		}
	}
	return nil
}

// SQLite BUSY means another Store owns the write lock; it is retryable and is
// deliberately distinct from a Provider CAS version conflict.
func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *modernsqlite.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code()&0xff == 5 || sqliteErr.Code()&0xff == 6) {
		return fmt.Errorf("%w: sqlite writer busy", providerapp.ErrStorageBusy)
	}
	return err
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) listModels(ctx context.Context, id string) ([]provider.Model, error) {
	return listModelsWith(ctx, s.db, id)
}

func listModelsWith(ctx context.Context, q sqlRunner, id string) ([]provider.Model, error) {
	rows, err := q.QueryContext(ctx, `SELECT model_id, display_name, is_default, context_window FROM provider_models WHERE provider_id = ? ORDER BY position, model_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []provider.Model{}
	for rows.Next() {
		var m provider.Model
		var cw sql.NullInt64
		if err := rows.Scan(&m.ModelID, &m.DisplayName, &m.IsDefault, &cw); err != nil {
			return nil, err
		}
		if cw.Valid {
			m.ContextWindow = cw.Int64
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
