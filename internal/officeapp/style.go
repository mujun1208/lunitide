package officeapp

import (
	"context"
	"encoding/json"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func StyleFromCheckpoint(raw json.RawMessage) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	var style string
	if json.Unmarshal(fields["styleId"], &style) != nil {
		return ""
	}
	return NormalizeTaskStyle(style)
}

func WithTaskStyle(previous json.RawMessage, style string) json.RawMessage {
	style = NormalizeTaskStyle(style)
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(previous, &fields)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	if style == "" {
		delete(fields, "styleId")
	} else {
		fields["styleId"] = encode(style)
	}
	return encode(fields)
}

func NormalizeTaskStyle(style string) string {
	style = strings.TrimSpace(style)
	if _, ok := content.LoadTemplate(style); ok {
		return style
	}
	return ""
}

func (s *Service) SetTaskStyle(ctx context.Context, taskID, style string) error {
	task, err := s.Store.GetOfficeTask(ctx, taskID)
	if err != nil {
		return err
	}
	style = NormalizeTaskStyle(style)
	if style == "" {
		return domain.ErrInvalid
	}
	task.Checkpoint = WithTaskStyle(task.Checkpoint, style)
	_, err = s.Store.UpdateOfficeTask(ctx, task, task.Revision)
	return err
}

func applyTaskStyle(task domain.Task, spec content.Spec) content.Spec {
	if strings.TrimSpace(spec.TemplateID) != "" {
		return spec
	}
	if style := StyleFromCheckpoint(task.Checkpoint); style != "" && content.TemplateSupportsKind(style, spec.Kind) {
		spec.TemplateID = style
	}
	return spec
}
