package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/officeapp"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func officeToolDefinitions() []llmadapter.ToolDefinition {
	text := map[string]any{"type": "string"}
	obj := func(p map[string]any, r ...string) map[string]any {
		if r == nil {
			r = []string{}
		}
		return map[string]any{"type": "object", "additionalProperties": false, "properties": p, "required": r}
	}
	arr := func(items any, max int) map[string]any {
		return map[string]any{"type": "array", "maxItems": max, "items": items}
	}
	boundedText := func(max int) map[string]any { return map[string]any{"type": "string", "maxLength": max} }
	emu := map[string]any{"type": "integer", "minimum": 0, "maximum": 12192000}
	image := obj(map[string]any{"sourceId": text, "sha256": boundedText(64), "x": emu, "y": emu, "width": emu, "height": emu, "fit": map[string]any{"enum": []string{"contain", "cover"}}, "alt": boundedText(2048)}, "sourceId", "sha256", "x", "y", "width", "height", "fit", "alt")
	image["description"] = "Use an existing current-session PNG/JPEG attachment ID and exact SHA256, never a URL or path. Maximum 8 MiB/24 megapixels per image. Coordinates in EMU (914400/inch), within the slide. contain keeps the full image; cover crops proportionally."
	chartType := map[string]any{"enum": []string{"column", "bar", "line", "pie"}}
	chart := obj(map[string]any{"type": chartType, "title": boundedText(1024), "categories": arr(boundedText(1024), 100), "series": arr(obj(map[string]any{"name": boundedText(1024), "values": arr(boundedText(64), 100)}, "name", "values"), 6), "x": emu, "y": emu, "width": emu, "height": emu, "legend": map[string]any{"type": "boolean"}}, "type", "title", "categories", "series", "x", "y", "width", "height", "legend")
	chart["description"] = "Native editable chart backed by an embedded workbook. Values are decimal STRINGS with at most 15 significant digits, never rounded JSON floats. Each series must match the category count. Pie requires one nonnegative nonzero series. For replacements keep all geometry unchanged or use zeros to preserve the source frame."
	slide := obj(map[string]any{"title": boundedText(100), "subtitle": boundedText(300), "layout": map[string]any{"enum": []string{"cover", "section", "content", "two-column", "comparison", "quote", "metrics", "timeline", "agenda", "closing", "table"}, "description": "metrics and timeline allow at most 6 bullet items; other non-table layouts allow 12. table requires rows and no bullets. Rows are forbidden in other layouts."}, "bullets": arr(boundedText(300), 12), "notes": text, "rows": arr(arr(boundedText(160), 6), 9)}, "title")
	slide["description"] = "PPT content is never silently truncated. table rows must be rectangular with 1–9 rows and 1–6 columns. Use metrics items as value|label. Split oversized content into additional slides."
	slide["properties"].(map[string]any)["images"] = arr(image, 16)
	slide["properties"].(map[string]any)["charts"] = arr(chart, 8)
	document := obj(map[string]any{"header": text, "footer": text, "pageNumbers": map[string]any{"type": "boolean"}, "pageNumberStart": map[string]any{"type": "integer", "minimum": 0, "maximum": 32767}, "pageSize": map[string]any{"enum": []string{"A4", "Letter"}}, "orientation": map[string]any{"enum": []string{"portrait", "landscape"}}})
	block := obj(map[string]any{"type": map[string]any{"enum": []string{"heading", "heading2", "heading3", "paragraph", "bullet", "numbered", "quote", "caption", "table", "pagebreak", "toc", "section"}}, "text": text, "rows": arr(arr(text, 16), 500), "section": document}, "type")
	block["description"] = "Word: table requires rectangular rows (1–500 rows, 1–16 columns, at most 10,000 table cells across the document). Only table accepts rows. table/toc/pagebreak/section must omit text. section requires section settings; all other types must omit section. At most 64 document sections."
	cell := obj(map[string]any{"type": map[string]any{"enum": []string{"text", "number", "boolean", "formula", "date", "blank"}}, "value": text, "format": text}, "type")
	cell["description"] = "value is always a string, at most 32,767 UTF-8 bytes; text preserves identifiers and literal = prefixes. number is decimal text with at most 15 significant digits; boolean is true/false; date is YYYY-MM-DD in 1900–9999; blank must omit value. Only formula executes a local-workbook formula. format is at most 128 UTF-8 bytes."
	sheet := obj(map[string]any{"name": text, "rows": arr(arr(cell, 128), 5000), "freezeHeader": map[string]any{"type": "boolean"}}, "name", "rows")
	sheetChart := obj(map[string]any{"type": chartType, "title": boundedText(1024), "categories": boundedText(64), "series": arr(obj(map[string]any{"name": boundedText(1024), "range": boundedText(64)}, "name", "range"), 6), "anchor": boundedText(32), "width": map[string]any{"type": "integer", "minimum": 160, "maximum": 1920}, "height": map[string]any{"type": "integer", "minimum": 120, "maximum": 1440}, "legend": map[string]any{"type": "boolean"}}, "type", "title", "categories", "series", "anchor", "width", "height", "legend")
	sheetChart["description"] = "Native Excel chart. Categories and each series range must be a current-sheet single-column A1 range with the same 1–100 rows. Numeric source cells must be explicit number type, never stale formula caches. Anchor is a cell address; size is pixels. Range edits update supported source-linked charts in the same new version."
	sheet["properties"].(map[string]any)["charts"] = arr(sheetChart, 8)
	spec := obj(map[string]any{"schemaVersion": map[string]any{"const": 1}, "kind": map[string]any{"enum": []string{"pptx", "docx", "xlsx", "pdf"}}, "title": text, "slides": arr(slide, 30), "blocks": arr(block, 500), "sheets": arr(sheet, 16), "body": text, "document": document}, "schemaVersion", "kind", "title")
	spec["description"] = "Title must be nonempty and at most 1,024 UTF-8 bytes. Supply only matching content: pptx requires 1–30 slides; docx requires 1–500 blocks and optional document settings; xlsx requires 1–16 sheets with 1–5,000 rows each, at most 128 columns and 50,000 total cells; pdf uses body. Omit content fields belonging to other kinds."
	rangeCell := obj(map[string]any{"type": map[string]any{"enum": []string{"text", "number", "boolean", "formula", "date", "blank"}}, "value": text}, "type")
	rangeCell["description"] = "Explicit cell type/value follows generation rules; format is not supported by range editing. Existing styles are preserved."
	rangePatch := obj(map[string]any{"part": text, "range": text, "expectedDigest": text, "rows": arr(arr(rangeCell, 128), 1000)}, "part", "range", "expectedDigest", "rows")
	rangePatch["description"] = "Rows must exactly match the inclusive rectangular A1 range. This tool accepts at most 1,000 rows and 128 columns per range, 32 ranges and 50,000 changed cells in total. Ranges cannot overlap. No format changes; rich-text, merged follower, shared/array formula cells remain read-only."
	definition := func(name, description string, schema any) llmadapter.ToolDefinition {
		b, _ := json.Marshal(schema)
		return llmadapter.ToolDefinition{Name: name, Description: description, Schema: b}
	}
	return []llmadapter.ToolDefinition{
		definition("office.chart.patch", "Replace real managed PPT chart data/title/type in a new version, preserving the original frame and unrelated objects. Read all office.inspect view=chart pages first and use the exact version/node digest/head revision. External or complex charts remain read-only.", obj(map[string]any{"taskId": text, "versionId": text, "expectedRevision": map[string]any{"type": "integer", "minimum": 1}, "nodeId": text, "nodeDigest": text, "chart": chart}, "versionId", "expectedRevision", "nodeId", "nodeDigest", "chart")),
		definition("office.cache.refresh", "Use local LibreOffice to refresh supported Word field displays or Excel formula caches into a new version. Original parts, formulas and inputs are preserved by selective merge. Requires exact inspected version and head revision. Missing renderer, unsupported fields or calculation errors fail explicitly; it does not prove Office/WPS layout compatibility.", obj(map[string]any{"taskId": text, "versionId": text, "expectedRevision": map[string]any{"type": "integer", "minimum": 1}}, "versionId", "expectedRevision")),
		definition("office.image.replace", "Replace one inspected simple embedded PPT picture using a real current-session attachment and exact SHA256. Preserves the frame and unrelated package parts, creates a new version. First inspect node id/digest and current head revision. Unsupported pictures stay read-only.", obj(map[string]any{"taskId": text, "versionId": text, "expectedRevision": map[string]any{"type": "integer", "minimum": 1}, "nodeId": text, "nodeDigest": text, "attachmentId": text, "sha256": boundedText(64), "fit": map[string]any{"enum": []string{"contain", "cover"}}, "alt": boundedText(2048)}, "versionId", "expectedRevision", "nodeId", "nodeDigest", "attachmentId", "sha256", "fit")),
		definition("office.generate", "Create a real versioned Office Studio file and a session delivery copy. Reuse the current Office task; include complete content, not placeholders. Formats pptx/docx/xlsx/pdf. XLSX identifiers and imported strings MUST use type=text, even starting =; only explicit formulas use type=formula. Stored file and QA are separate; never claim target rendering verified unless returned evidence says so. Existing simpler generators remain available.", obj(map[string]any{"taskId": text, "name": text, "spec": spec}, "name", "spec")),
		definition("office.inspect", "Read current Office sources or inspect before edits. Returns complete bounded JSON. No versionId: page versions with offset=nextOffset. For reading, use view=text with versionId, then textOffset=nextTextOffset until hasMore=false; PDF falls back to local OCR, marked as uncertain. Prefer an uploaded editable original over its PDF copy. For edits, use view=nodes with nodeOffset=nextNodeOffset AND textOffset=nextTextOffset for exact IDs/digests. view=parts pages OOXML package names/digests; PDF parts being empty does NOT mean blank/no text. view=chart requires versionId/nodeId/nodeDigest and returns chartJSON fragments; concatenate in textOffset order. Structure does not prove rendered layout.", obj(map[string]any{"taskId": text, "versionId": text, "nodeId": text, "nodeDigest": text, "view": map[string]any{"enum": []string{"text", "nodes", "parts", "chart"}}, "offset": map[string]any{"type": "integer", "minimum": 0}, "nodeOffset": map[string]any{"type": "integer", "minimum": 0}, "textOffset": map[string]any{"type": "integer", "minimum": 0}})),
		definition("office.patch", "Modify supported text nodes only, preserving all untouched OOXML parts. First inspect for exact versionId/nodeId/digest and artifact head revision. Publishes a new version, never overwrites source. Rich text, numbers and formulas can be read-only. Missing support must be reported.", obj(map[string]any{"taskId": text, "versionId": text, "expectedRevision": map[string]any{"type": "integer", "minimum": 1}, "nodeId": text, "nodeDigest": text, "text": text}, "versionId", "expectedRevision", "nodeId", "nodeDigest", "text")),
		definition("office.range.patch", "Edit explicit typed XLSX ranges. First office.inspect for exact worksheet part digest; preserve non-target cells and styles. Invalidates dependent formula caches and reports incomplete recalculation. Text IDs keep leading zeroes. Publishes a new immutable version.", obj(map[string]any{"taskId": text, "versionId": text, "expectedRevision": map[string]any{"type": "integer", "minimum": 1}, "ranges": arr(rangePatch, 32)}, "versionId", "expectedRevision", "ranges")),
		definition("office.deliver", "Manage source-backed metrics and fixed-version delivery bundles. capture reads exact node (never invent rawValue); apply inserts a metric using template with exactly one {{value}}. metrics pages with offset=nextOffset and textOffset=nextTextOffset; metricId selects one metric for reading long values. bundle fixes explicit versionIds and returns its id. bundles pages summaries with offset, or bundleId plus fileOffset=nextFileOffset to read every fixed file. Outputs stay within the tool budget. Source updates mark drafts stale; accepted versions remain unchanged.", obj(map[string]any{"taskId": text, "action": map[string]any{"enum": []string{"metrics", "capture", "apply", "bundles", "bundle"}}, "offset": map[string]any{"type": "integer", "minimum": 0}, "textOffset": map[string]any{"type": "integer", "minimum": 0}, "fileOffset": map[string]any{"type": "integer", "minimum": 0}, "bundleId": text, "versionId": text, "nodeId": text, "nodeDigest": text, "name": text, "unit": text, "currency": text, "period": text, "roundingDigits": map[string]any{"type": "integer", "minimum": 0, "maximum": 12}, "metricId": text, "targetVersionId": text, "targetNodeId": text, "targetNodeDigest": text, "template": text, "expectedRevision": map[string]any{"type": "integer", "minimum": 1}, "title": text, "versionIds": arr(text, 32)}, "action")),
	}
}

func (e *Engine) executeOfficeTool(ctx context.Context, sessionID, name string, args json.RawMessage) ([]byte, string, string, error) {
	var p struct {
		TaskID           string               `json:"taskId"`
		Name             string               `json:"name"`
		Spec             content.Spec         `json:"spec"`
		AttachmentID     string               `json:"attachmentId"`
		SHA256           string               `json:"sha256"`
		Fit              string               `json:"fit"`
		Alt              *string              `json:"alt"`
		VersionID        string               `json:"versionId"`
		ExpectedRevision int64                `json:"expectedRevision"`
		NodeID           string               `json:"nodeId"`
		NodeOffset       int                  `json:"nodeOffset"`
		Offset           int                  `json:"offset"`
		TextOffset       int                  `json:"textOffset"`
		FileOffset       int                  `json:"fileOffset"`
		View             string               `json:"view"`
		BundleID         string               `json:"bundleId"`
		NodeDigest       string               `json:"nodeDigest"`
		Text             string               `json:"text"`
		Ranges           []content.RangePatch `json:"ranges"`
		Chart            content.SlideChart   `json:"chart"`
		Action           string               `json:"action"`
		Unit             string               `json:"unit"`
		Currency         string               `json:"currency"`
		Period           string               `json:"period"`
		RoundingDigits   *int                 `json:"roundingDigits"`
		MetricID         string               `json:"metricId"`
		TargetVersionID  string               `json:"targetVersionId"`
		TargetNodeID     string               `json:"targetNodeId"`
		TargetNodeDigest string               `json:"targetNodeDigest"`
		Template         string               `json:"template"`
		Title            string               `json:"title"`
		VersionIDs       []string             `json:"versionIds"`
	}
	if decodePayload(args, &p) != nil {
		return nil, "", "", domain.ErrInvalid
	}
	orgID, _, err := e.boundOrgState(ctx)
	if err != nil {
		return nil, "", "", err
	}
	ctx = domain.WithScope(ctx, orgID)
	s := e.officeStudio
	if s == nil || (!e.officeCapabilities().Studio && name != "office.inspect") {
		return nil, "", "", fmt.Errorf("FEATURE_DISABLED: 办公文件生成和修改已关闭")
	}
	var task domain.Task
	if bound := officeTaskContextID(ctx); bound != "" {
		if p.TaskID != "" && p.TaskID != bound {
			return nil, "", "", domain.ErrScope
		}
		p.TaskID = bound
	}
	if p.TaskID != "" {
		task, err = s.Store.GetOfficeTask(ctx, p.TaskID)
	} else {
		var items []domain.Task
		items, err = s.Store.ListOfficeTasks(ctx, sessionID, 1)
		if err == nil && len(items) > 0 {
			task = items[0]
		}
		if err == nil && task.ID == "" {
			if name == "office.inspect" {
				b, _ := json.Marshal(map[string]any{"taskId": "", "heads": []domain.Head{}, "versions": []domain.Version{}, "notice": "当前会话尚无办公任务或文件；检查不会自动创建任务。"})
				return nil, "", string(b), nil
			}
			task, err = s.Store.CreateOfficeTask(ctx, domain.Task{SessionID: sessionID, Title: "办公文件", Status: "draft"}, "office-chat-"+sessionID)
		}
	}
	if err != nil {
		return nil, "", "", err
	}
	if task.SessionID != sessionID {
		return nil, "", "", domain.ErrScope
	}
	if name == "office.inspect" {
		if p.View == "chart" {
			output, err := e.officeChartPage(ctx, task.ID, p.VersionID, p.NodeID, p.NodeDigest, p.TextOffset)
			return nil, "", output, err
		}
		output, err := e.officeInspectPage(ctx, task.ID, p.VersionID, p.View, p.Offset, p.NodeOffset, p.TextOffset)
		return nil, "", output, err
	}
	var version domain.Version
	key := toolruntime.ExecutionKey(ctx)
	if key == "" {
		key = ulid.Make().String()
	}
	if name == "office.deliver" {
		var result any
		switch p.Action {
		case "metrics":
			result, err = s.ListMetrics(ctx, task.ID)
		case "bundles":
			result, err = s.ListBundles(ctx, task.ID)
		case "capture":
			result, err = s.CaptureMetric(ctx, task.ID, officeapp.MetricCapture{SourceVersionID: p.VersionID, SourceNodeID: p.NodeID, SourceNodeDigest: p.NodeDigest, Name: p.Name, Unit: p.Unit, Currency: p.Currency, Period: p.Period, RoundingDigits: p.RoundingDigits}, key)
		case "bundle":
			if !e.officeCapabilities().Bundle {
				return nil, "", "", fmt.Errorf("FEATURE_DISABLED: delivery bundles disabled")
			}
			result, err = s.CreateBundle(ctx, task.ID, p.Title, p.VersionIDs, key)
		case "apply":
			err = s.Execute(ctx, task.ID, "patch", func(run context.Context) error {
				v, _, er := s.ReadVersion(run, task.ID, p.TargetVersionID)
				if er != nil {
					return er
				}
				if !e.officeWritesEnabled("office.patch", v.Kind) {
					return domain.ErrInvalid
				}
				version, er = s.ApplyMetric(run, task.ID, officeapp.MetricApply{MetricID: p.MetricID, TargetVersionID: p.TargetVersionID, TargetNodeID: p.TargetNodeID, TargetNodeDigest: p.TargetNodeDigest, Template: p.Template, ExpectedRevision: p.ExpectedRevision}, key)
				return er
			})
		default:
			return nil, "", "", domain.ErrInvalid
		}
		if err != nil {
			return nil, "", "", err
		}
		if p.Action != "apply" {
			output, err := officeDeliveryPage(task.ID, p.Action, p.MetricID, p.BundleID, p.Offset, p.TextOffset, p.FileOffset, result)
			return nil, "", output, err
		}
	}
	if version.ID != "" {
		return e.officeToolFile(ctx, task.ID, version)
	}
	if name == "office.generate" {
		if !e.officeWritesEnabled(name, string(p.Spec.Kind)) {
			return nil, "", "", fmt.Errorf("FEATURE_DISABLED: Office generation disabled")
		}
		if err = e.hydrateOfficeImages(ctx, task.SessionID, &p.Spec); err != nil {
			return nil, "", "", err
		}
		err = s.Execute(ctx, task.ID, "running", func(run context.Context) error {
			var runErr error
			version, runErr = s.Generate(run, task.ID, p.Name, p.Spec, key)
			return runErr
		})
	} else {
		var v domain.Version
		v, _, err = s.ReadVersion(ctx, task.ID, p.VersionID)
		if err != nil {
			return nil, "", "", err
		}
		if !e.officeWritesEnabled("office.patch", v.Kind) {
			return nil, "", "", fmt.Errorf("FEATURE_DISABLED: Office patch disabled")
		}
		err = s.Execute(ctx, task.ID, "running", func(run context.Context) error {
			var runErr error
			patch := content.PatchRequest{Kind: content.Kind(v.Kind), BaseSHA256: v.SHA256}
			switch name {
			case "office.cache.refresh":
				if !e.officeCapabilities().Render {
					return fmt.Errorf("FEATURE_DISABLED: native renderer disabled")
				}
				version, runErr = s.RefreshNativeCaches(run, task.ID, v.ID, p.ExpectedRevision, key)
				return runErr
			case "office.range.patch":
				patch.Ranges = p.Ranges
			case "office.chart.patch":
				if v.Kind != "pptx" {
					return domain.ErrInvalid
				}
				patch.Charts = []content.ChartPatch{{NodeID: p.NodeID, ExpectedDigest: p.NodeDigest, Chart: p.Chart}}
			case "office.image.replace":
				if v.Kind != "pptx" {
					return domain.ErrInvalid
				}
				data, er := e.officeImageSource(run, task.SessionID, p.AttachmentID, p.SHA256)
				if er != nil {
					return er
				}
				patch.Images = []content.ImagePatch{{NodeID: p.NodeID, ExpectedDigest: p.NodeDigest, SourceID: p.AttachmentID, SHA256: p.SHA256, Fit: p.Fit, Alt: p.Alt, Data: data}}
			default:
				patch.Operations = []content.TextPatch{{NodeID: p.NodeID, ExpectedDigest: p.NodeDigest, Text: p.Text}}
			}
			version, runErr = s.Patch(run, task.ID, v.ID, p.ExpectedRevision, patch, key)
			return runErr
		})
	}
	if err != nil {
		return nil, "", "", err
	}
	return e.officeToolFile(ctx, task.ID, version)
}

func (e *Engine) officeToolFile(ctx context.Context, taskID string, version domain.Version) ([]byte, string, string, error) {
	_, b, err := e.officeStudio.ReadVersion(ctx, taskID, version.ID)
	if err != nil {
		return nil, "", "", err
	}
	fileName := strings.TrimSuffix(version.Name, filepath.Ext(version.Name)) + "-" + version.ID[len(version.ID)-6:] + filepath.Ext(version.Name)
	summary := fmt.Sprintf("文件已存档：%s；版本 %d；taskId=%s；versionId=%s；质量状态=%s（目标软件排版未验证）。", version.Name, version.VersionNo, taskID, version.ID, version.Quality)
	if audit := officeToolAudit(version); audit != "" {
		summary += "\n实际修改与计算检查：" + audit
	}
	return b, fileName, summary, nil
}
