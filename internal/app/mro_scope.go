package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/mroapp"
)

type mroResponseError struct{ response bridge.Response }

func (e *mroResponseError) Error() string { return "MRO operation refused" }

func mroMutation(method string) bool {
	return !strings.HasSuffix(method, ".list") && method != "mro.lot.trace" && method != "mro.kit.staging" && method != "mro.plan.constraint.check" && method != "mro.checklist.build"
}
func (e *Engine) handleScopedMRO(ctx context.Context, r bridge.Request, handler func(*Engine, context.Context, bridge.Request) bridge.Response) bridge.Response {
	orgID, err := e.boundOrgID(ctx)
	if err != nil {
		return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认，请重试", true)
	}
	ctx = mroapp.WithScope(ctx, orgID)
	if !mroMutation(r.Method) {
		return handler(e, ctx, r)
	}
	if e.mro == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "机务工作台暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	if r.Method == "mro.plan.publish" || r.Method == "mro.manual.register" {
		var p struct {
			Documents []struct {
				DocumentID string `json:"documentId"`
			} `json:"documents"`
		}
		if err = json.Unmarshal(r.Payload, &p); err != nil {
			return mroFailure(r, mroapp.ErrPayloadInvalid)
		}
		var ids []string
		for _, doc := range p.Documents {
			ids = append(ids, doc.DocumentID)
		}
		ctx, err = e.mro.PrepareEvidence(ctx, ids)
		if err != nil {
			return mroFailure(r, err)
		}
	}
	var payload any
	if json.Unmarshal(r.Payload, &payload) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "机务参数无效", false)
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "机务参数无效", false)
	}
	digest := sha256.Sum256(normalized)
	out, err := e.mro.ExecuteRequest(ctx, r.Method, r.IdempotencyKey, hex.EncodeToString(digest[:]), func(txCtx context.Context) (json.RawMessage, error) {
		response := handler(e, txCtx, r)
		if !response.OK {
			return nil, &mroResponseError{response}
		}
		return json.Marshal(response.Payload)
	}, func(replayCtx context.Context) error {
		if r.Method == "mro.plan.publish" {
			var p struct {
				PackageID string `json:"packageId"`
			}
			if err := json.Unmarshal(r.Payload, &p); err != nil {
				return err
			}
			return e.mro.VerifyPublication(replayCtx, p.PackageID)
		}
		return nil
	})
	if err != nil {
		var failure *mroResponseError
		if errors.As(err, &failure) {
			return failure.response
		}
		return mroFailure(r, err)
	}
	return r.Ok(json.RawMessage(out))
}
