package command

import "errors"

// Sentinel errors for the command gateway. Each carries its stable wire
// family so callers can map refusals without parsing text.
var (
	// ErrSpecSignature is CMD-001: manifest JSON damaged or the ed25519
	// signature over the canonical manifest does not verify.
	ErrSpecSignature = errors.New("command: spec manifest signature invalid (CMD-001)")
	// ErrSpecExpired is CMD-001: the signed manifest lapsed past ExpiresAt.
	ErrSpecExpired = errors.New("command: spec manifest expired (CMD-001)")
	// ErrSpecRevoked is CMD-001: a spec digest appears in the manifest
	// revocation list; the spec is skipped and the error aggregated.
	ErrSpecRevoked = errors.New("command: spec revoked by manifest (CMD-001)")
	// ErrParamInvalid is CMD-002: a start parameter is missing, unknown or
	// fails its schema type check; the message names the offending field.
	ErrParamInvalid = errors.New("command: start parameter invalid (CMD-002)")
	// ErrEnvNotAllowed is CMD-002: an environment key outside the spec
	// allowlist; the message names the offending key.
	ErrEnvNotAllowed = errors.New("command: environment key not allowed (CMD-002)")
	// ErrCwdOutsideWorkspace is CMD-002: the requested cwd is not inside
	// the workspace root under the "workspace" cwd policy.
	ErrCwdOutsideWorkspace = errors.New("command: cwd outside workspace (CMD-002)")
	// ErrTemplateUnknown is CMD-002: an argv template placeholder refers to
	// a parameter that was not supplied.
	ErrTemplateUnknown = errors.New("command: argv template placeholder unknown (CMD-002)")
	// ErrUnsupported reports platform-absent job execution.
	ErrUnsupported = errors.New("command: unsupported on this platform")
	// ErrOrphaned is TASK-001: an orphaned job tree was reaped via its
	// kill-on-close Job Object handle.
	ErrOrphaned = errors.New("command: job orphaned and reaped (TASK-001)")
)
