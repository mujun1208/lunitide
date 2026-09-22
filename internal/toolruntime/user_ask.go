package toolruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const userAskTool = "user.ask"

var userAskReasons = map[string]bool{
	"login": true, "2fa": true, "captcha": true, "pay": true,
	"uac": true, "file_picker": true, "decision": true,
}

type userAskArgs struct {
	Title     string            `json:"title,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	Questions []userAskQuestion `json:"questions"`
}

type userAskQuestion struct {
	ID      string          `json:"id,omitempty"`
	Prompt  string          `json:"prompt"`
	Options []userAskOption `json:"options"`
}

type userAskOption struct {
	ID          string `json:"id,omitempty"`
	Label       string `json:"label"`
	Recommended bool   `json:"recommended,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

type looseUserAskOption struct {
	ID          string          `json:"id"`
	Label       string          `json:"label"`
	Recommended json.RawMessage `json:"recommended"`
	Detail      string          `json:"detail"`
	Description string          `json:"description"`
}

const userAskDetailMax = 160

// NormalizeUserAsk rewrites a decision card so the wizard always has exactly
// one recommended option, keeps a one-line consequence, and drops fields the
// schema does not know. description is accepted as an alias of detail.
func NormalizeUserAsk(args json.RawMessage) (json.RawMessage, error) {
	var loose struct {
		Title     string `json:"title"`
		Reason    string `json:"reason"`
		Questions []struct {
			ID      string               `json:"id"`
			Prompt  string               `json:"prompt"`
			Options []looseUserAskOption `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(args, &loose) != nil {
		return nil, errors.New("invalid arguments")
	}
	out := userAskArgs{Title: strings.TrimSpace(loose.Title), Reason: strings.TrimSpace(loose.Reason)}
	for _, q := range loose.Questions {
		nq := userAskQuestion{ID: strings.TrimSpace(q.ID), Prompt: strings.TrimSpace(q.Prompt)}
		picked := -1
		for _, opt := range q.Options {
			detail := strings.TrimSpace(opt.Detail)
			if detail == "" {
				detail = strings.TrimSpace(opt.Description)
			}
			detail = truncateRunes(detail, userAskDetailMax)
			no := userAskOption{
				ID:     strings.TrimSpace(opt.ID),
				Label:  strings.TrimSpace(opt.Label),
				Detail: detail,
			}
			if picked < 0 && flagTrue(opt.Recommended) {
				no.Recommended = true
				picked = len(nq.Options)
			}
			nq.Options = append(nq.Options, no)
		}
		if picked < 0 && len(nq.Options) > 0 {
			nq.Options[0].Recommended = true
		}
		out.Questions = append(out.Questions, nq)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, errors.New("invalid arguments")
	}
	if err := validateUserAsk(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func flagTrue(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "true", `"true"`, "1":
		return true
	default:
		return false
	}
}

func executeUserAsk(args json.RawMessage, approved bool) (Result, error) {
	if err := validateUserAsk(args); err != nil {
		return Result{}, err
	}
	if !approved {
		return Result{}, ErrApprovalRequired
	}
	return result("用户已提交决策。请根据随后用户消息中的选项继续，不要重复提问。"), nil
}

func validateUserAsk(args json.RawMessage) error {
	var a userAskArgs
	if strict(args, &a) != nil {
		return errors.New("invalid arguments")
	}
	if strings.TrimSpace(a.Title) != "" && utf8.RuneCountInString(a.Title) > 200 {
		return errors.New("invalid arguments")
	}
	if reason := strings.TrimSpace(a.Reason); reason != "" && !userAskReasons[reason] {
		return errors.New("invalid arguments")
	}
	if len(a.Questions) < 1 || len(a.Questions) > 8 {
		return errors.New("invalid arguments")
	}
	seen := map[string]bool{}
	for _, q := range a.Questions {
		prompt := strings.TrimSpace(q.Prompt)
		if prompt == "" || utf8.RuneCountInString(prompt) > 500 {
			return errors.New("invalid arguments")
		}
		id := strings.TrimSpace(q.ID)
		if id == "" {
			id = prompt
		}
		if seen[id] {
			return errors.New("invalid arguments")
		}
		seen[id] = true
		if len(q.Options) < 2 || len(q.Options) > 5 {
			return errors.New("invalid arguments")
		}
		optSeen := map[string]bool{}
		for _, opt := range q.Options {
			label := strings.TrimSpace(opt.Label)
			if label == "" || utf8.RuneCountInString(label) > 200 {
				return errors.New("invalid arguments")
			}
			if utf8.RuneCountInString(strings.TrimSpace(opt.Detail)) > userAskDetailMax {
				return errors.New("invalid arguments")
			}
			oid := strings.TrimSpace(opt.ID)
			if oid == "" {
				oid = label
			}
			if oid == "__other__" || optSeen[oid] {
				return errors.New("invalid arguments")
			}
			optSeen[oid] = true
		}
	}
	return nil
}

// UserAskApprovalSummary is the compact JSON the renderer parses into the
// decision wizard. Empty when the arguments are invalid. The card always
// carries exactly one recommended option.
func UserAskApprovalSummary(args json.RawMessage) string {
	norm, err := NormalizeUserAsk(args)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, norm); err != nil {
		return strings.TrimSpace(string(norm))
	}
	return buf.String()
}
