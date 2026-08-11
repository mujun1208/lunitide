package compactionapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
)

// GatewaySummarizerConfig configures the gateway-backed summarizer.
type GatewaySummarizerConfig struct {
	// Model is the LLM model ID to use for summarization (e.g., "gpt-4o-mini").
	Model string
	// MaxTokens is the max output tokens for the summary response.
	MaxTokens int
	// SystemPrompt is the system instruction for the summarizer.
	SystemPrompt string
}

// DefaultGatewaySummarizerConfig returns sensible defaults for summarization.
func DefaultGatewaySummarizerConfig(model string) GatewaySummarizerConfig {
	return GatewaySummarizerConfig{
		Model:     model,
		MaxTokens: 2048,
		SystemPrompt: `You are a conversation summarizer. Summarize the following conversation segment.
If a PRIOR SUMMARY is provided, incorporate it into a new rolling summary that preserves
all critical facts from both the prior summary and the new messages.
Preserve all critical facts: ULIDs, file paths, code snippets, quoted text, decisions, and action items.
Output a JSON object with these fields:
{
  "summary": "concise plain-text summary of the conversation",
  "keyPoints": ["array of key decisions and facts"],
  "actionItems": ["array of pending action items"],
  "entities": {"type": "array of mentioned entities with context"}
}
Keep the summary under 500 words. Do not omit any ULIDs, paths, or code blocks mentioned.`,
	}
}

// ProviderLookup looks up a provider by ID for the summarizer to obtain
// credentials and adapter configuration.
type ProviderLookup interface {
	GetProvider(ctx context.Context, id string) (provider.Provider, error)
}

// LeaseAcquirer acquires a secret lease for the summarizer's gateway call.
type LeaseAcquirer interface {
	WithLease(ctx context.Context, req secretlease.Request, fn func(secret []byte) error) error
}

// AdapterFactory creates a gateway adapter for the given provider.
type AdapterFactory interface {
	Adapter(ctx context.Context, p provider.Provider) (gateway.Adapter, error)
}

// GatewaySummarizer implements the Summarizer interface by calling an LLM
// via the gateway. It is used by the compaction executor to generate
// structured summaries of conversation segments.
type GatewaySummarizer struct {
	providers ProviderLookup
	leases    LeaseAcquirer
	adapters  AdapterFactory
	config    GatewaySummarizerConfig
	timeout   time.Duration
}

// NewGatewaySummarizer creates a new gateway-backed summarizer.
func NewGatewaySummarizer(providers ProviderLookup, leases LeaseAcquirer, adapters AdapterFactory, config GatewaySummarizerConfig) *GatewaySummarizer {
	return &GatewaySummarizer{
		providers: providers,
		leases:    leases,
		adapters:  adapters,
		config:    config,
		timeout:   90 * time.Second,
	}
}

// SetTimeout allows overriding the default summarization timeout.
func (s *GatewaySummarizer) SetTimeout(d time.Duration) { s.timeout = d }

// Summarize implements the Summarizer interface by calling the LLM via gateway.Complete.
// It converts SummaryMessages to gateway messages, calls the LLM, and returns
// the raw response as both summaryJSON and humanSummary.
// If priorSummary is non-empty, it is included in the prompt so the LLM can
// produce a rolling/incremental summary that preserves facts from earlier
// compaction rounds (ADR-005 §3 RollingSummary).
func (s *GatewaySummarizer) Summarize(ctx context.Context, sessionID, providerID, modelID string, sourceStartSeq, sourceEndSeq int64, messages []SummaryMessage, priorSummary string) (string, string, error) {
	if len(messages) == 0 {
		return "", "", ErrNoMessagesToSummarize
	}

	// Look up the provider for summarization.
	prov, err := s.providers.GetProvider(ctx, providerID)
	if err != nil {
		return "", "", fmt.Errorf("summarizer provider lookup: %w", err)
	}

	// Build gateway messages from the conversation segment.
	gwMessages := make([]gateway.Message, 0, len(messages)+1)
	gwMessages = append(gwMessages, gateway.Message{
		Role:    gateway.RoleSystem,
		Content: s.config.SystemPrompt,
	})

	var conversationBuilder strings.Builder
	conversationBuilder.WriteString("Conversation segment to summarize (messages ")
	conversationBuilder.WriteString(fmt.Sprintf("%d-%d):\n\n", sourceStartSeq, sourceEndSeq))

	// Include prior summary for rolling/incremental compaction (ADR-005 §3).
	if priorSummary != "" {
		conversationBuilder.WriteString("=== PRIOR SUMMARY (from previous compaction) ===\n")
		conversationBuilder.WriteString(priorSummary)
		conversationBuilder.WriteString("\n=== END PRIOR SUMMARY ===\n\n")
		conversationBuilder.WriteString("New messages to incorporate into the summary:\n\n")
	}

	for _, m := range messages {
		role := "User"
		if m.Role == "assistant" {
			role = "Assistant"
		}
		conversationBuilder.WriteString(fmt.Sprintf("[%s]: %s\n\n", role, m.Content))
	}

	gwMessages = append(gwMessages, gateway.Message{
		Role:    gateway.RoleUser,
		Content: conversationBuilder.String(),
	})

	// Determine model: use provided modelID, or config model, or provider default.
	model := modelID
	if model == "" {
		model = s.config.Model
	}
	if model == "" {
		for _, m := range prov.Models {
			if m.IsDefault {
				model = m.ModelID
				break
			}
		}
	}
	if model == "" && len(prov.Models) > 0 {
		model = prov.Models[0].ModelID
	}
	if model == "" {
		return "", "", fmt.Errorf("no model available for summarization")
	}

	// Acquire lease and call the LLM.
	summCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	var summaryJSON, humanSummary string

	origin, err := provider.NormalizeOrigin(prov.BaseURL)
	if err != nil {
		return "", "", fmt.Errorf("normalize origin: %w", err)
	}

	deadline := time.Now().Add(s.timeout)
	leaseErr := s.leases.WithLease(summCtx, secretlease.Request{
		ProviderID:    prov.ID,
		CredentialRef: prov.CredentialRef,
		Origin:        origin,
		Protocol:      string(prov.Protocol),
		Operation:     secretlease.OperationChat,
		Deadline:      deadline,
	}, func(secret []byte) error {
		adapter, adapterErr := s.adapters.Adapter(summCtx, prov)
		if adapterErr != nil {
			return fmt.Errorf("create adapter: %w", adapterErr)
		}

		resp, completeErr := adapter.Complete(summCtx, secret, gateway.Request{
			Model:       model,
			Messages:    gwMessages,
			MaxTokens:   s.config.MaxTokens,
			MaxAttempts: 1,
		})
		if completeErr != nil {
			return fmt.Errorf("llm complete: %w", completeErr)
		}

		content := resp.Message.Content
		// Try to parse as JSON; if it fails, use raw content as humanSummary.
		var structured map[string]any
		if jsonErr := json.Unmarshal([]byte(content), &structured); jsonErr == nil {
			summaryJSON = content
			if summary, ok := structured["summary"].(string); ok {
				humanSummary = summary
			} else {
				humanSummary = content
			}
		} else {
			// LLM didn't return valid JSON; wrap it.
			wrapped := map[string]any{
				"summary":     content,
				"keyPoints":   []string{},
				"actionItems": []string{},
			}
			data, _ := json.Marshal(wrapped)
			summaryJSON = string(data)
			humanSummary = content
		}
		return nil
	})

	if leaseErr != nil {
		return "", "", leaseErr
	}

	return summaryJSON, humanSummary, nil
}
