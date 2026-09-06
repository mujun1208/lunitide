package m7app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/workspace"
	"github.com/oklog/ulid/v2"
)

// LocalPublicationReceipt describes actual files in the protected release
// directory. It is never an external deployment/health receipt.
type LocalPublicationReceipt struct {
	Kind         string                 `json:"kind"`
	PromotionID  string                 `json:"promotionId"`
	ProjectID    string                 `json:"projectId"`
	BlobDigest   string                 `json:"blobDigest"`
	IntentDigest string                 `json:"intentDigest"`
	TargetEnv    string                 `json:"targetEnv"`
	Members      []m7flow.PackageMember `json:"members"`
}

func publicationRoot(root string) (*workspace.SecureRoot, error) {
	return workspace.NewSecureRoot(root)
}
func publicationRead(root *workspace.SecureRoot, name string, limit int64) ([]byte, error) {
	f, err := root.OpenSecure(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, ErrPackageInvalid
	}
	return data, err
}
func publicationPrefix(projectID, env, blob string) (string, error) {
	if _, err := ulid.ParseStrict(projectID); err != nil {
		return "", ErrPackageInvalid
	}
	if env != "dev" && env != "stage" {
		return "", ErrPolicyRejected
	}
	if len(blob) != 64 || !isLowerHex(blob) {
		return "", ErrPackageInvalid
	}
	return path.Join(projectID, env, blob), nil
}
func publicationMemberPath(prefix string, m m7flow.PackageMember) (string, error) {
	if m.Name == "" || path.Base(m.Name) != m.Name || strings.ContainsAny(m.Name, "\\:") || workspace.ValidateRelPath(m.Name) != nil {
		return "", ErrPackageInvalid
	}
	return path.Join(prefix, "members", m.Name), nil
}

// VerifyLocalPublication reopens both the persisted receipt and each member.
// Caller receipts cannot prove publication without matching protected files.
func VerifyLocalPublication(ctx context.Context, rootPath, receipt string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(receipt) > 65536 {
		return ErrPackageInvalid
	}
	var r LocalPublicationReceipt
	if err := json.Unmarshal([]byte(receipt), &r); err != nil {
		return err
	}
	if r.Kind != "local-artifact-publication-v1" || len(r.Members) < 1 || len(r.Members) > 32 {
		return ErrPackageInvalid
	}
	if _, err := ulid.ParseStrict(r.PromotionID); err != nil {
		return ErrPackageInvalid
	}
	prefix, err := publicationPrefix(r.ProjectID, r.TargetEnv, r.BlobDigest)
	if err != nil {
		return err
	}
	root, err := publicationRoot(rootPath)
	if err != nil {
		return err
	}
	if revoked, openErr := root.OpenSecure(path.Join("receipts", r.PromotionID+".revoked")); openErr == nil {
		_ = revoked.Close()
		return ErrRollbackNotAllowed
	} else if !errors.Is(openErr, os.ErrNotExist) {
		return openErr
	}
	saved, err := publicationRead(root, path.Join("receipts", r.PromotionID+".json"), 65536)
	if err != nil {
		return err
	}
	if string(saved) != receipt {
		return ErrDigestMismatch
	}
	manifest, err := publicationRead(root, path.Join(prefix, "package.json"), 1<<20)
	if err != nil {
		return err
	}
	if m7flow.SHA256Hex(manifest) != r.BlobDigest {
		return ErrDigestMismatch
	}
	var doc m7flow.SealedPackageDoc
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return err
	}
	if doc.Manifest["projectId"] != r.ProjectID || m7flow.Digest256(doc.Members) != m7flow.Digest256(r.Members) {
		return ErrDigestMismatch
	}
	for _, member := range r.Members {
		if err := ctx.Err(); err != nil {
			return err
		}
		if member.Size < 1 || member.Size > 10<<20 {
			return ErrPackageInvalid
		}
		name, err := publicationMemberPath(prefix, member)
		if err != nil {
			return err
		}
		data, err := publicationRead(root, name, 10<<20)
		if err != nil {
			return err
		}
		if int64(len(data)) != member.Size || m7flow.SHA256Hex(data) != member.SHA256 {
			return ErrDigestMismatch
		}
	}
	return nil
}

type localArtifactDeployment struct {
	root string
	tx   PromotionTx
}

func (d localArtifactDeployment) Plan(ctx context.Context, packageID, blob, env string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if env == m7flow.EnvProd {
		return "", ErrPolicyRejected
	}
	return planDigest("local-artifacts", packageID, blob, env), nil
}
func (d localArtifactDeployment) Dispatch(ctx context.Context, promotionID, blob, intent string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	prm, err := d.tx.GetPromotion(promotionID)
	if err != nil {
		return "", err
	}
	raw, err := d.tx.GetReleaseBlob(blob)
	if err != nil {
		return "", err
	}
	if len(raw) > 1<<20 || m7flow.SHA256Hex([]byte(raw)) != blob {
		return "", ErrDigestMismatch
	}
	var doc m7flow.SealedPackageDoc
	if err = json.Unmarshal([]byte(raw), &doc); err != nil {
		return "", err
	}
	if err = VerifyReleaseContent(d.tx, doc); err != nil {
		return "", err
	}
	projectID, _ := doc.Manifest["projectId"].(string)
	prefix, err := publicationPrefix(projectID, prm.ToEnv, blob)
	if err != nil {
		return "", err
	}
	root, err := publicationRoot(d.root)
	if err != nil {
		return "", err
	}
	for _, m := range doc.Members {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		name, err := publicationMemberPath(prefix, m)
		if err != nil {
			return "", err
		}
		bytes, err := ReadReleaseMember(d.tx, m.SHA256, m.Size)
		if err != nil {
			return "", err
		}
		if err = root.WriteAtomic(name, bytes, 0600); err != nil {
			return "", err
		}
	}
	if err = root.WriteAtomic(path.Join(prefix, "package.json"), []byte(raw), 0600); err != nil {
		return "", err
	}
	receipt := LocalPublicationReceipt{"local-artifact-publication-v1", promotionID, projectID, blob, intent, prm.ToEnv, doc.Members}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	// Publication becomes visible only after every file is durable. No global
	// environment pointer is overwritten, so rollback cannot disturb another
	// project or a concurrently published newer release.
	if err = root.WriteAtomic(path.Join("receipts", promotionID+".json"), encoded, 0600); err != nil {
		return "", err
	}
	if err = VerifyLocalPublication(ctx, d.root, string(encoded)); err != nil {
		return "", err
	}
	return string(encoded), nil
}
func (d localArtifactDeployment) Verify(ctx context.Context, id, receipt string) error {
	var r LocalPublicationReceipt
	if err := json.Unmarshal([]byte(receipt), &r); err != nil {
		return err
	}
	if r.PromotionID != id {
		return ErrDigestMismatch
	}
	return VerifyLocalPublication(ctx, d.root, receipt)
}
func (d localArtifactDeployment) Rollback(ctx context.Context, id, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := ulid.ParseStrict(id); err != nil {
		return err
	}
	root, err := publicationRoot(d.root)
	if err != nil {
		return err
	}
	// Append a durable revocation marker; immutable package bytes remain for
	// audit and are not reported as a live external environment rollback.
	return root.WriteAtomic(path.Join("receipts", id+".revoked"), []byte("local publication revoked"), 0600)
}

// This adapter explicitly records that a local file publication changes no
// database schema. It cannot accept or execute external migration scripts.
type localArtifactMigration struct{ root string }

func (m localArtifactMigration) Plan(ctx context.Context, packageID, blob, env string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if env == m7flow.EnvProd {
		return "", "", ErrPolicyRejected
	}
	return planDigest("local-no-schema", packageID, blob, env), planDigest("local-no-schema-rollback", packageID, blob, env), nil
}
func (m localArtifactMigration) Apply(ctx context.Context, id, plan, intent string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := ulid.ParseStrict(id); err != nil {
		return "", err
	}
	root, err := publicationRoot(m.root)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(map[string]string{"kind": "local-no-schema-change", "plan": plan, "intent": intent})
	if err = root.WriteAtomic(path.Join("receipts", id+"-migration.json"), data, 0600); err != nil {
		return "", err
	}
	return "local-no-schema:" + id, nil
}
func (m localArtifactMigration) Verify(ctx context.Context, id, plan string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := ulid.ParseStrict(id); err != nil {
		return err
	}
	root, err := publicationRoot(m.root)
	if err != nil {
		return err
	}
	data, err := publicationRead(root, path.Join("receipts", id+"-migration.json"), 4096)
	if err != nil {
		return err
	}
	var doc map[string]string
	if err = json.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc["kind"] != "local-no-schema-change" || doc["plan"] != plan {
		return ErrDigestMismatch
	}
	return nil
}
func (m localArtifactMigration) Rollback(ctx context.Context, id, ref string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ref != "local-no-schema:"+id {
		return fmt.Errorf("%w: local migration receipt", ErrDigestMismatch)
	}
	return nil
}
