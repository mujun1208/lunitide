package modelquality

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/lunitide/lunitide/internal/modelfit"
)

var (
	reasoningAssign  = regexp.MustCompile(`(?i)(reasoning(?:_content|Content)?\s*[=:]\s*).+$`)
	protocolPrivate  = regexp.MustCompile(`(?i)(protocol\s+private\b[:\s]*).+$`)
)

type RuntimeEvidence struct {
	API            json.RawMessage
	Logs           []string
	BackupManifest []string
	BackupCipher   []byte
	Protocol       modelfit.ProtocolCapture
}

func RedactRuntimeEvidence(in RuntimeEvidence) RuntimeEvidence {
	out := RuntimeEvidence{
		API:          redactEvidenceJSON(in.API),
		BackupCipher: append([]byte(nil), in.BackupCipher...),
		Protocol:     modelfit.RedactProtocolCapture(in.Protocol),
	}
	for _, line := range in.Logs {
		out.Logs = append(out.Logs, redactEvidenceText(line))
	}
	for _, line := range in.BackupManifest {
		out.BackupManifest = append(out.BackupManifest, redactEvidenceText(line))
	}
	return out
}

func redactEvidenceJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return json.RawMessage(redactEvidenceText(string(raw)))
	}
	redactPrivateValue(v)
	out, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(redactEvidenceText(string(raw)))
	}
	return json.RawMessage(out)
}

func redactPrivateValue(v any) {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if isPrivateEvidenceKey(k) {
				n[k] = "[redacted]"
				continue
			}
			if s, ok := child.(string); ok {
				n[k] = SanitizeLog(s)
				continue
			}
			redactPrivateValue(child)
		}
	case []any:
		for i, child := range n {
			if s, ok := child.(string); ok {
				n[i] = SanitizeLog(s)
				continue
			}
			redactPrivateValue(child)
		}
	}
}

func isPrivateEvidenceKey(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "reasoning", "reasoningcontent", "reasoning_content", "protocolprivate", "protocol_private":
		return true
	default:
		return false
	}
}

func redactEvidenceText(s string) string {
	s = SanitizeLog(s)
	if json.Valid([]byte(s)) {
		return string(redactEvidenceJSON([]byte(s)))
	}
	s = protocolPrivate.ReplaceAllString(s, "${1}[redacted]")
	s = reasoningAssign.ReplaceAllString(s, "${1}[redacted]")
	return s
}
