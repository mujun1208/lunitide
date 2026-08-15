package command

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/workspace"
)

// CwdPolicyWorkspace pins the process cwd inside the workspace root.
const CwdPolicyWorkspace = "workspace"

// Clock is the injectable time source (same contract as workspace.Clock).
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// StartInput is one command.start request after spec resolution.
type StartInput struct {
	Spec          CommandSpec
	Args          map[string]string
	Env           map[string]string
	Cwd           string
	WorkspaceRoot string
}

// ValidateStart enforces the CMD-002 contract: parameters match the schema
// (typed, bounded, workspace-relative paths), environment keys stay inside
// the spec allowlist, the cwd stays inside the workspace root and the argv
// template renders without unknown placeholders.
func ValidateStart(in StartInput) error {
	if err := validateParams(in.Spec, in.Args); err != nil {
		return err
	}
	if err := validateEnv(in.Spec, in.Env); err != nil {
		return err
	}
	if err := validateCwd(in.Spec, in.Cwd, in.WorkspaceRoot); err != nil {
		return err
	}
	if _, err := RenderArgv(in.Spec, in.Args); err != nil {
		return err
	}
	return nil
}

// validateParams rejects unknown and missing parameters, then type-checks
// every supplied value. Errors name the offending field (CMD-002).
func validateParams(spec CommandSpec, args map[string]string) error {
	for name := range args {
		if _, ok := spec.ParamSchema[name]; !ok {
			return fmt.Errorf("%w: field %s not in schema", ErrParamInvalid, name)
		}
	}
	var missing []string
	for name, p := range spec.ParamSchema {
		val, ok := args[name]
		if !ok {
			if p.Required {
				missing = append(missing, name)
			}
			continue
		}
		if err := validateParamValue(p, val); err != nil {
			return fmt.Errorf("%w: field %s %v", ErrParamInvalid, name, err)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("%w: field %s required but missing", ErrParamInvalid, strings.Join(missing, ","))
	}
	return nil
}

func validateParamValue(p ParamSpec, val string) error {
	switch p.Type {
	case ParamString:
		if p.MaxLen > 0 && len(val) > p.MaxLen {
			return fmt.Errorf("length %d exceeds maxLen %d", len(val), p.MaxLen)
		}
	case ParamInt:
		if _, err := strconv.Atoi(val); err != nil {
			return fmt.Errorf("not an int: %q", val)
		}
	case ParamBool:
		if val != "true" && val != "false" {
			return fmt.Errorf("not bool: %q", val)
		}
	case ParamPath:
		if err := workspace.ValidateRelPath(val); err != nil {
			return fmt.Errorf("path rejected: %q", val)
		}
	default:
		return fmt.Errorf("unknown type %q", p.Type)
	}
	return nil
}

// validateEnv rejects any environment key outside the spec allowlist.
func validateEnv(spec CommandSpec, env map[string]string) error {
	allowed := make(map[string]bool, len(spec.EnvAllowlist))
	for _, k := range spec.EnvAllowlist {
		allowed[k] = true
	}
	for k := range env {
		if !allowed[k] {
			return fmt.Errorf("%w: key %s not in spec allowlist", ErrEnvNotAllowed, k)
		}
	}
	return nil
}

// validateCwd enforces the cwd policy: under "workspace" the cleaned cwd
// must equal or sit below the cleaned workspace root (prefix plus boundary
// separator), which also rejects .. traversal out of the root.
func validateCwd(spec CommandSpec, cwd, root string) error {
	if spec.CwdPolicy != CwdPolicyWorkspace {
		return nil
	}
	cleanRoot := filepath.Clean(root)
	cleanCwd := filepath.Clean(cwd)
	sep := string(os.PathSeparator)
	lowerCwd := strings.ToLower(cleanCwd)
	lowerRoot := strings.ToLower(cleanRoot)
	if lowerCwd != lowerRoot && !strings.HasPrefix(lowerCwd, lowerRoot+sep) {
		return fmt.Errorf("%w: %s escapes workspace root %s", ErrCwdOutsideWorkspace, cwd, root)
	}
	return nil
}

// placeholderRe matches one {name} placeholder inside a template item.
var placeholderRe = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)

// RenderArgv expands the {arg} placeholders of the spec template with the
// supplied values. Substitution is per-item and single-pass: a value never
// re-expands, never splits into further argv entries and no shell ever
// interprets the result. An unknown placeholder name answers
// ErrTemplateUnknown (CMD-002).
func RenderArgv(spec CommandSpec, args map[string]string) ([]string, error) {
	out := make([]string, 0, len(spec.ArgvTemplate))
	for _, tok := range spec.ArgvTemplate {
		var renderErr error
		rendered := placeholderRe.ReplaceAllStringFunc(tok, func(m string) string {
			name := m[1 : len(m)-1]
			val, ok := args[name]
			if !ok {
				if renderErr == nil {
					renderErr = fmt.Errorf("%w: placeholder %s has no value", ErrTemplateUnknown, name)
				}
				return m
			}
			return val
		})
		if renderErr != nil {
			return nil, renderErr
		}
		out = append(out, rendered)
	}
	return out, nil
}
