package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/secretlease"
)

var (
	errExpertTrialLength   = errors.New("expert trial reached completion limit")
	errExpertTrialFiltered = errors.New("expert trial was content filtered")
)

func handleExpertDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p m8app.ExpertDeleteInput
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) || !validCanonicalULID(p.ExpectedVersionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "expert.delete 参数无效", false)
	}
	if err := e.m8expert.Delete(ctx, p); err != nil {
		return m8ExpertFailure(r, err)
	}
	return r.Ok(map[string]any{"expertId": p.ExpertID, "deleted": true})
}

func handleExpertTry(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ExpertID          string `json:"expertId"`
		ExpectedVersionID string `json:"expectedVersionId"`
		Input             string `json:"input"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ExpertID) || !validCanonicalULID(p.ExpectedVersionID) || strings.TrimSpace(p.Input) == "" || len([]rune(p.Input)) > 8000 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "expert.try 参数无效", false)
	}
	snapshot, err := e.m8expert.PrepareTrial(ctx, p.ExpertID, p.ExpectedVersionID)
	if err != nil {
		return m8ExpertFailure(r, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	output, err := e.completeExpertTrial(ctx, snapshot, p.Input)
	if err != nil {
		if errors.Is(err, errExpertTrialLength) {
			return r.Fail("EXPERT_TRIAL_INCOMPLETE", "专家试答达到输出限制，内容未完成，请缩短样例后重试", true)
		}
		if errors.Is(err, errExpertTrialFiltered) {
			return r.Fail("EXPERT_TRIAL_FILTERED", "专家试答被模型内容过滤，未返回完整回答", false)
		}
		return r.Fail("EXPERT_TRIAL_FAILED", "专家试答未完成，请检查模型连接后重试", true)
	}
	return r.Ok(map[string]any{"expertId": snapshot.ExpertID, "versionId": snapshot.VersionID, "name": snapshot.Name, "state": snapshot.State, "output": output})
}

func (e *Engine) completeExpertTrial(ctx context.Context, snapshot m8app.ExpertTrial, input string) (string, error) {
	if e.providers == nil {
		return "", errors.New("expert trial model unavailable")
	}
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	entry, ok := e.resolvePreferredChatModel(items)
	if !ok {
		return "", errors.New("expert trial model unavailable")
	}
	body, err := json.Marshal(snapshot.SixSection)
	if err != nil {
		return "", err
	}
	var output string
	err = e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		adapter, err := e.adapter(op, entry.Provider)
		if err != nil {
			return err
		}
		response, err := adapter.Complete(op, secret, llmadapter.Request{
			Model: entry.Model.ModelID, MaxTokens: 2000, MaxAttempts: 1, DisableReasoning: true,
			Messages: []llmadapter.Message{
				{Role: "system", Content: "You are testing the expert profile " + snapshot.Name + ". Answer the user's sample according to all six sections below. Give a concise, complete answer with the requested ending; output only the final answer, without reasoning or a preamble. This is a text-only trial: no tools, files, browsing or skill execution are available. Do not claim any actions were performed. Do not enable, install or modify the expert.\n" + string(body)},
				{Role: "user", Content: input},
			},
		})
		if err != nil {
			return err
		}
		switch response.FinishReason {
		case llmadapter.FinishReasonLength:
			return errExpertTrialLength
		case llmadapter.FinishReasonContentFilter:
			return errExpertTrialFiltered
		}
		output = strings.TrimSpace(response.Message.Content)
		if output == "" || len(response.Message.ToolCalls) > 0 || response.FinishReason == llmadapter.FinishReasonToolCalls {
			return errors.New("expert trial returned no text answer")
		}
		return nil
	})
	return output, err
}
