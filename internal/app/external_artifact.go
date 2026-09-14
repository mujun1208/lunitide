package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/agenthub"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

type ExternalArtifactClaim struct {
	Workspace   string
	Path        string
	ExitCode    int
	Kind        string
	UsageTokens *int64
	TaskID      string
	VersionID   string
	Policy      domain.DeliveryPolicy
}

type ExternalArtifactResult struct {
	Delivered      bool
	Reason         string
	UsageIntegrity string
	FileSHA256     string
	Decision       domain.FormalDecision
}

func (e *Engine) VerifyExternalExecutorArtifact(ctx context.Context, claim ExternalArtifactClaim) (ExternalArtifactResult, error) {
	out := ExternalArtifactResult{UsageIntegrity: "unknown"}
	if claim.UsageTokens != nil {
		out.UsageIntegrity = "reported"
	}
	abs, err := filepath.Abs(claim.Path)
	if err != nil || !agenthub.PathAllowed(claim.Workspace, "", abs) {
		out.Reason = "path_escape"
		return out, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			out.Reason = "missing_file"
			return out, err
		}
		out.Reason = "missing_file"
		return out, err
	}
	sum := sha256.Sum256(data)
	out.FileSHA256 = hex.EncodeToString(sum[:])
	if _, err = content.Inspect(content.Kind(claim.Kind), data); err != nil {
		out.Reason = "inspect_failed"
		return out, err
	}
	if out.UsageIntegrity != "reported" {
		out.Reason = "usage_unknown"
		return out, nil
	}
	if e.officeStudio == nil || claim.TaskID == "" || claim.VersionID == "" {
		out.Reason = "needs_formal"
		return out, nil
	}
	dec, err := e.officeStudio.AssessDelivery(ctx, claim.TaskID, claim.VersionID, claim.Policy)
	if err != nil {
		return out, err
	}
	out.Decision = dec
	if dec.Allowed && dec.State == "verified" {
		out.Delivered = true
		return out, nil
	}
	out.Reason = "needs_formal"
	return out, nil
}
