package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/fileops"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func fileOpsToolDefinitions() []llmadapter.ToolDefinition {
	return []llmadapter.ToolDefinition{
		{Name: "files.plan", Description: "Create a confined workspace file plan. recipe=classify moves listed files into extension folders; weekly-report writes a markdown week report from notes[] and workspace files[]; briefing writes a short public brief from bullets[]. Or pass items[{action,from,to,body}] with action=mkdir|rename|move|write. Returns planId. Apply with files.apply, undo with files.undo.", Schema: []byte(`{"type":"object","properties":{"recipe":{"type":"string","enum":["classify","weekly-report","briefing"]},"files":{"type":"array","maxItems":200,"items":{"type":"string"}},"dest":{"type":"string"},"title":{"type":"string"},"notes":{"type":"array","maxItems":40,"items":{"type":"string"}},"bullets":{"type":"array","maxItems":40,"items":{"type":"string"}},"items":{"type":"array","maxItems":200,"items":{"type":"object","additionalProperties":false,"properties":{"action":{"type":"string","enum":["mkdir","rename","move","write"]},"from":{"type":"string"},"to":{"type":"string"},"body":{"type":"string"}},"required":["action"]}}},"additionalProperties":false}`)},
		{Name: "files.apply", Description: "Apply one files.plan by planId. Stops on the first failed step and keeps earlier succeeded items for undo.", Schema: []byte(`{"type":"object","properties":{"planId":{"type":"string","minLength":1}},"required":["planId"],"additionalProperties":false}`)},
		{Name: "files.status", Description: "Read the current state of a files.plan.", Schema: []byte(`{"type":"object","properties":{"planId":{"type":"string","minLength":1}},"required":["planId"],"additionalProperties":false}`)},
		{Name: "files.undo", Description: "Undo a files.plan that was applied or partially applied. Restores moved files and deletes files this plan created.", Schema: []byte(`{"type":"object","properties":{"planId":{"type":"string","minLength":1}},"required":["planId"],"additionalProperties":false}`)},
	}
}

var errFilesArgs = errors.New("文件操作参数无效")
var errWorkspaceUnavailable = errors.New("工作区尚未就绪")
var errUnknownFilesTool = errors.New("未知文件操作")

func decodeFilePlanItems(raw []json.RawMessage) ([]fileops.Item, error) {
	items := make([]fileops.Item, 0, len(raw))
	for _, row := range raw {
		dec := json.NewDecoder(bytes.NewReader(row))
		dec.DisallowUnknownFields()
		var in struct {
			Action fileops.Action `json:"action"`
			From   string         `json:"from"`
			To     string         `json:"to"`
			Body   string         `json:"body"`
		}
		if err := dec.Decode(&in); err != nil {
			return nil, errors.New("文件计划参数无效")
		}
		items = append(items, fileops.Item{Action: in.Action, From: in.From, To: in.To, Body: in.Body})
	}
	return items, nil
}

func fileOpsUserError(err error) error {
	if err == nil {
		return nil
	}
	msg := fileops.UserMessage(err)
	switch {
	case errors.Is(err, fileops.ErrInputChanged):
		return fmt.Errorf("%s: %w", msg, fileops.ErrInputChanged)
	case errors.Is(err, fileops.ErrCollision):
		return fmt.Errorf("%s: %w", msg, fileops.ErrCollision)
	case errors.Is(err, fileops.ErrUndoCollision):
		return fmt.Errorf("%s: %w", msg, fileops.ErrUndoCollision)
	case errors.Is(err, fileops.ErrNothingToUndo):
		return fmt.Errorf("%s: %w", msg, fileops.ErrNothingToUndo)
	case errors.Is(err, fileops.ErrCrossVolume):
		return fmt.Errorf("%s: %w", msg, fileops.ErrCrossVolume)
	case errors.Is(err, fileops.ErrOutsideRoot):
		return fmt.Errorf("%s: %w", msg, fileops.ErrOutsideRoot)
	case errors.Is(err, fileops.ErrPlanNotFound):
		return fmt.Errorf("%s: %w", msg, fileops.ErrPlanNotFound)
	case errors.Is(err, fileops.ErrInvalidPlan):
		return fmt.Errorf("%s: %w", msg, fileops.ErrInvalidPlan)
	case errors.Is(err, fileops.ErrApplyPartial):
		return fmt.Errorf("%s: %w", msg, fileops.ErrApplyPartial)
	case errors.Is(err, fileops.ErrAlreadyUndone):
		return fmt.Errorf("%s: %w", msg, fileops.ErrAlreadyUndone)
	case errors.Is(err, fileops.ErrSourceMissing):
		return fmt.Errorf("%s: %w", msg, fileops.ErrSourceMissing)
	case errors.Is(err, fileops.ErrUnavailable):
		return fmt.Errorf("%s: %w", msg, fileops.ErrUnavailable)
	case errors.Is(err, fileops.ErrInvalidRoot):
		return fmt.Errorf("%s: %w", msg, fileops.ErrInvalidRoot)
	default:
		if msg != "" && msg != err.Error() {
			return errors.New(msg)
		}
		return err
	}
}

func (e *Engine) fileOpsFor(session string) (*fileops.Service, error) {
	if e != nil && e.fileOps != nil {
		return e.fileOps, nil
	}
	if e == nil || e.tools == nil || session == "" {
		return nil, errWorkspaceUnavailable
	}
	svc, err := fileops.New(filepath.Join(e.tools.WorkspaceRoot(), session))
	if err != nil {
		return nil, fileOpsUserError(err)
	}
	return svc, nil
}

func (e *Engine) executeFilesBridgeTool(ctx context.Context, session, name string, args json.RawMessage) (toolruntime.Result, error) {
	if name == "files.status" {
		return e.executeFileOps(ctx, session, name, args)
	}
	return e.executeUserTool(ctx, executionModeFullAccess, session, name, args)
}

func (e *Engine) executeFileOps(ctx context.Context, session, name string, args json.RawMessage) (toolruntime.Result, error) {
	svc, err := e.fileOpsFor(session)
	if err != nil {
		return toolruntime.Result{}, err
	}
	switch name {
	case "files.plan":
		var p struct {
			Recipe  string            `json:"recipe"`
			Files   []string          `json:"files"`
			Dest    string            `json:"dest"`
			Title   string            `json:"title"`
			Notes   []string          `json:"notes"`
			Bullets []string          `json:"bullets"`
			Items   []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(args, &p) != nil {
			return toolruntime.Result{}, errors.New("文件计划参数无效")
		}
		items, err := decodeFilePlanItems(p.Items)
		if err != nil {
			return toolruntime.Result{}, err
		}
		switch p.Recipe {
		case "classify":
			items = append(items, fileops.ClassifyRename(p.Files)...)
		case "weekly-report":
			notes, noteErr := weeklyReportNotes(svc, p.Notes, p.Files)
			if noteErr != nil {
				return toolruntime.Result{}, noteErr
			}
			items = append(items, fileops.WeeklyReport(p.Dest, p.Title, notes)...)
		case "briefing":
			items = append(items, fileops.Briefing(p.Dest, p.Title, p.Bullets)...)
		}
		plan, err := svc.Create(p.Recipe, items)
		if err != nil {
			return toolruntime.Result{}, fileOpsUserError(err)
		}
		body, _ := json.Marshal(plan)
		return toolruntime.Result{Output: string(body)}, nil
	case "files.apply":
		var p struct {
			PlanID string `json:"planId"`
		}
		if json.Unmarshal(args, &p) != nil || p.PlanID == "" {
			return toolruntime.Result{}, errFilesArgs
		}
		st, err := svc.Apply(ctx, p.PlanID)
		body, _ := json.Marshal(st)
		if err != nil {
			return toolruntime.Result{Output: string(body)}, fileOpsUserError(err)
		}
		return toolruntime.Result{Output: string(body)}, nil
	case "files.status":
		var p struct {
			PlanID string `json:"planId"`
		}
		if json.Unmarshal(args, &p) != nil || p.PlanID == "" {
			return toolruntime.Result{}, errFilesArgs
		}
		st, err := svc.Status(p.PlanID)
		if err != nil {
			return toolruntime.Result{}, fileOpsUserError(err)
		}
		body, _ := json.Marshal(st)
		return toolruntime.Result{Output: string(body)}, nil
	case "files.undo":
		var p struct {
			PlanID string `json:"planId"`
		}
		if json.Unmarshal(args, &p) != nil || p.PlanID == "" {
			return toolruntime.Result{}, errFilesArgs
		}
		st, err := svc.Undo(ctx, p.PlanID)
		body, _ := json.Marshal(st)
		if err != nil {
			return toolruntime.Result{Output: string(body)}, fileOpsUserError(err)
		}
		return toolruntime.Result{Output: string(body)}, nil
	default:
		return toolruntime.Result{}, errUnknownFilesTool
	}
}

func weeklyReportNotes(svc *fileops.Service, notes, files []string) ([]string, error) {
	out := append([]string{}, notes...)
	for _, rel := range files {
		text, err := svc.ReadBoundedText(rel, 16<<10)
		if err != nil {
			return nil, fileOpsUserError(err)
		}
		if strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	if len(files) > 0 && len(out) == 0 {
		return nil, errors.New("周报没有可用的参考材料正文")
	}
	return out, nil
}
