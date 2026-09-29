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
	Title     string            `json:"title"`
	Reason    string            `json:"reason"`
	Questions []userAskQuestion `json:"questions"`
}

type userAskQuestion struct {
	ID      string          `json:"id"`
	Prompt  string          `json:"prompt"`
	Options []userAskOption `json:"options"`
}

type userAskOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
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
// decision wizard. Empty when the arguments are invalid.
func UserAskApprovalSummary(args json.RawMessage) string {
	if validateUserAsk(args) != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, args); err != nil {
		return strings.TrimSpace(string(args))
	}
	return buf.String()
}
