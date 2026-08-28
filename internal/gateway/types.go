// Package gateway implements bounded, provider-neutral LLM gateway adapters.
package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"toolCallId,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Image is trusted binary input assembled from a session-scoped attachment.
type Image struct {
	MIME string
	Data []byte
}

type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type Request struct {
	Model            string
	Messages         []Message
	Images           []Image
	MaxTokens        int
	MaxAttempts      int
	IdempotencyKey   string
	Tools            []ToolDefinition
	DisableReasoning bool // 月伴模式：跳过推理/思考内容，直接流式输出文本
}

type Response struct {
	Message   Message `json:"message"`
	Usage     Usage   `json:"usage"`
	Reasoning string  `json:"reasoning,omitempty"`
}

type Delta struct {
	Text      string    `json:"text,omitempty"`
	Reasoning string    `json:"reasoning,omitempty"`
	Usage     *Usage    `json:"usage,omitempty"`
	ToolCall  *ToolCall `json:"toolCall,omitempty"`
}

type Model struct {
	ID string `json:"id"`
}

type Discovery struct {
	Models      []Model `json:"models"`
	Unsupported bool    `json:"unsupported,omitempty"`
	Warning     string  `json:"warning,omitempty"`
}

type Stage string

const (
	StageConnect Stage = "connect"
	StageHTTP    Stage = "http"
	StageDecode  Stage = "decode"
)

type Error struct {
	Code       string `json:"code"`
	Stage      Stage  `json:"stage"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	Message    string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

type TestResult struct {
	OK               bool          `json:"ok"`
	Stage            Stage         `json:"stage"`
	HTTPStatus       int           `json:"httpStatus,omitempty"`
	Latency          time.Duration `json:"latency"`
	Error            *Error        `json:"error,omitempty"`
	SanitizedMessage string        `json:"sanitizedMessage"`
}

// Connector is deliberately the narrow surface of networkpolicy.Connector.
// Production callers must pass that connector; adapters never allocate clients/transports.
type Connector interface {
	NewRequest(context.Context, string, string, io.Reader) (*http.Request, error)
	Do(*http.Request) (*http.Response, error)
	ReadSSE(io.Reader) (event []byte, eof bool, err error)
}

type ImageGenerator interface {
	GenerateImage(context.Context, []byte, string, string) (MediaResult, error)
}

type VideoGenerator interface {
	GenerateVideo(context.Context, []byte, string, string) (MediaResult, error)
}

type MediaResult struct {
	URL  string
	MIME string
	Data []byte
	ID   string
}

type Adapter interface {
	Complete(context.Context, []byte, Request) (Response, error)
	Stream(context.Context, []byte, Request, func(Delta) error) (Response, error)
	Discover(context.Context, []byte) (Discovery, error)
}

// ConnectionTester verifies that the provider accepts an authenticated
// request without requiring a full chat-completion response body.
type ConnectionTester interface {
	TestConnection(context.Context, []byte, Request) error
}

type Options struct {
	MaxRequestBytes   int
	MaxModels         int
	MaxAttempts       int
	RetryBase         time.Duration
	IdempotencyHeader string
}

func defaults(o Options) Options {
	if o.MaxRequestBytes <= 0 {
		o.MaxRequestBytes = 1 << 20
	}
	if o.MaxModels <= 0 || o.MaxModels > 50 {
		o.MaxModels = 50
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 1
	}
	if o.RetryBase <= 0 {
		o.RetryBase = 100 * time.Millisecond
	}
	return o
}
