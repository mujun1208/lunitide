package agenthub

import "time"

type AgentStatus struct {
	Name           string `json:"name"`
	State          string `json:"state"`
	Version        string `json:"version"`
	NonInteractive bool   `json:"nonInteractive"`
	StreamJSON     bool   `json:"streamJSON"`
	Hint           string `json:"hint"`
	Interactive    bool   `json:"interactive"`
	Protocol       string `json:"protocol"`
}

type TaskRequest struct {
	TaskID         string `json:"taskId"`
	Agent          string `json:"agent"`
	Prompt         string `json:"prompt"`
	WorkDir        string `json:"workDir"`
	Sandbox        string `json:"sandbox"`
	TimeoutMin     int    `json:"timeoutMin"`
	IdempotencyKey string `json:"idempotencyKey"`
}

type AgentEvent struct {
	Seq    int    `json:"seq"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	TS     string `json:"ts"`
	Path   string `json:"path,omitempty"`
	Tokens int64  `json:"tokens,omitempty"`
}

type Artifact struct {
	TaskID    string `json:"taskId,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	MIME      string `json:"mime"`
	Source    string `json:"source"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type TaskRecord struct {
	ID             string `json:"taskId"`
	Agent          string `json:"agent"`
	Prompt         string `json:"prompt"`
	WorkDir        string `json:"workDir"`
	Sandbox        string `json:"sandbox"`
	Status         string `json:"status"`
	ExitCode       *int64 `json:"exitCode"`
	TokensUsed     int64  `json:"tokensUsed"`
	ErrorMsg       string `json:"errorMsg"`
	CreatedAt      string `json:"createdAt"`
	StartedAt      string `json:"startedAt,omitempty"`
	FinishedAt     string `json:"finishedAt,omitempty"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

type TaskDetail struct {
	Task      TaskRecord   `json:"task"`
	Events    []AgentEvent `json:"events"`
	Artifacts []Artifact   `json:"artifacts"`
}

type TaskList struct {
	Items  []TaskRecord `json:"items"`
	Counts TaskCounts   `json:"counts"`
}

type TaskCounts struct {
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

type ListFilter struct {
	Agent    string
	Status   string
	DateFrom string
	DateTo   string
	Ext      string
}

type Capability struct {
	NonInteractive bool
	StreamJSON     bool
	Interactive    bool
	Protocol       string
}

type LookPath func(name string) (string, error)

type VersionRunner func(exe string, timeout time.Duration) (string, error)

type AgentAdapter interface {
	Name() string
	Detect(look LookPath, version VersionRunner) AgentStatus
	BuildCommand(req TaskRequest) (exe string, args []string, stdin []byte, err error)
	ParseLine(line string) (AgentEvent, bool)
}

type TaskStore interface {
	InsertTask(task TaskRecord) error
	GetTask(id string) (TaskRecord, error)
	GetTaskByKey(key string) (TaskRecord, error)
	UpdateTask(task TaskRecord) error
	ListTasks(filter ListFilter) ([]TaskRecord, TaskCounts, error)
	NextEventSeq(taskID string) (int, error)
	InsertEvent(taskID string, event AgentEvent) error
	ListEvents(taskID string) ([]AgentEvent, error)
	UpsertArtifact(taskID string, art Artifact) error
	ListArtifacts(taskID string) ([]Artifact, error)
	ListAllArtifacts(filter ListFilter) ([]Artifact, error)
}
