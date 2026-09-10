package modelfit

import (
	"fmt"
	"strings"
)

type Family string

const (
	FamilyDeepSeek  Family = "deepseek"
	FamilyGLM       Family = "glm"
	FamilyAnthropic Family = "anthropic"
)

const (
	CodecDeepSeekV1 = "deepseek-v1"
	CodecGLMV1      = "glm-v1"
)

type QualifyInput struct {
	Family Family
	Groups []MessageGroup
	Private ProtocolCapture
}

type Codec interface {
	Family() Family
	Version() string
	Qualify(QualifyInput) bool
	EncodeReplay([]MessageGroup, ProtocolCapture) ([]ProtocolMessage, error)
	RejectCrossFamily(Family) error
}

func LookupCodec(version string) Codec {
	switch version {
	case CodecDeepSeekV1:
		return familyCodec{family: FamilyDeepSeek, version: CodecDeepSeekV1}
	case CodecGLMV1:
		return familyCodec{family: FamilyGLM, version: CodecGLMV1}
	default:
		return nil
	}
}

type familyCodec struct {
	family  Family
	version string
}

func (c familyCodec) Family() Family { return c.family }
func (c familyCodec) Version() string { return c.version }

func (c familyCodec) Qualify(in QualifyInput) bool {
	if in.Family != c.family || in.Private.Source != SourceProvider || in.Private.ReasoningContent == "" {
		return false
	}
	if len(in.Groups) == 0 {
		return false
	}
	for _, g := range in.Groups {
		if !MessageGroupComplete(g) {
			return false
		}
	}
	return true
}

func (c familyCodec) RejectCrossFamily(src Family) error {
	if src == "" || src == c.family {
		return nil
	}
	return fmt.Errorf("refuse cross-family private fields: %s -> %s", src, c.family)
}

func (c familyCodec) EncodeReplay(groups []MessageGroup, private ProtocolCapture) ([]ProtocolMessage, error) {
	var out []ProtocolMessage
	for _, g := range groups {
		if !MessageGroupComplete(g) {
			return nil, fmt.Errorf("incomplete message group")
		}
		asst := g.Assistant
		if private.Source == SourceProvider && private.ReasoningContent != "" && asst.ReasoningContent == "" {
			asst.ReasoningContent = private.ReasoningContent
		}
		out = append(out, asst)
		out = append(out, g.Tools...)
	}
	return out, nil
}

func CodecForModel(model string, protocol string) string {
	m := strings.ToLower(model + " " + protocol)
	if strings.Contains(m, "deepseek") {
		return CodecDeepSeekV1
	}
	if strings.Contains(m, "glm") || strings.Contains(m, "zhipu") {
		return CodecGLMV1
	}
	return ""
}

func FamilyForCodec(version string) Family {
	if c := LookupCodec(version); c != nil {
		return c.Family()
	}
	return ""
}
