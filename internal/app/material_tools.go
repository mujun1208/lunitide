package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lunitide/lunitide/internal/dataprocess"
	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/imagebatch"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/lunitide/lunitide/internal/workspace"
)

func materialToolDefinitions() []llmadapter.ToolDefinition {
	return []llmadapter.ToolDefinition{
		{Name: "data.process", Description: "Filter, dedup, merge, stats, or export a workspace CSV/JSON/XLSX table. Writes a copy; never vendors DuckDB. Formula cells are rejected.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"otherPath":{"type":"string"},"op":{"type":"string","enum":["filter","dedup","merge","stats","export"]},"column":{"type":"string"},"equals":{"type":"string"},"keys":{"type":"array","maxItems":16,"items":{"type":"string"}},"out":{"type":"string"}},"required":["path","op"],"additionalProperties":false}`)},
		{Name: "image.batch", Description: "Crop, scale, compress, or watermark one workspace image and write a copy. GIF/animation is rejected.", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string","minLength":1},"op":{"type":"string","enum":["crop","scale","compress","watermark"]},"width":{"type":"integer","minimum":1},"height":{"type":"integer","minimum":1},"out":{"type":"string"}},"required":["path","op"],"additionalProperties":false}`)},
		{Name: "pdf.copy", Description: "Split or merge workspace PDFs. Copies are lossy text rerenders (layout and images are dropped). Forms and signatures are refused.", Schema: []byte(`{"type":"object","properties":{"op":{"type":"string","enum":["split","merge"]},"path":{"type":"string"},"files":{"type":"array","maxItems":16,"items":{"type":"string"}},"out":{"type":"string"}},"required":["op"],"additionalProperties":false}`)},
	}
}

func (e *Engine) executeMaterialTool(ctx context.Context, session, name string, args json.RawMessage) (toolruntime.Result, error) {
	if err := ctx.Err(); err != nil {
		return toolruntime.Result{}, err
	}
	root, err := e.materialWorkspaceRoot(session)
	if err != nil {
		return toolruntime.Result{}, err
	}
	switch name {
	case "data.process":
		return executeDataProcess(ctx, root, args)
	case "image.batch":
		return executeImageBatch(root, args)
	case "pdf.copy":
		return executePDFCopy(root, args)
	default:
		return toolruntime.Result{}, errors.New("未知材料操作")
	}
}

func (e *Engine) materialWorkspaceRoot(session string) (string, error) {
	if e == nil || e.tools == nil || strings.TrimSpace(session) == "" {
		return "", errWorkspaceUnavailable
	}
	return filepath.Join(e.tools.WorkspaceRoot(), session), nil
}

func executeDataProcess(ctx context.Context, root string, args json.RawMessage) (toolruntime.Result, error) {
	if err := ctx.Err(); err != nil {
		return toolruntime.Result{}, err
	}
	var p struct {
		Path      string   `json:"path"`
		OtherPath string   `json:"otherPath"`
		Op        string   `json:"op"`
		Column    string   `json:"column"`
		Equals    string   `json:"equals"`
		Keys      []string `json:"keys"`
		Out       string   `json:"out"`
	}
	if json.Unmarshal(args, &p) != nil {
		return toolruntime.Result{}, errors.New("数据处理参数无效")
	}
	raw, err := readWorkspaceFile(root, p.Path)
	if err != nil {
		return toolruntime.Result{}, err
	}
	tab, err := importTable(p.Path, raw)
	if err != nil {
		return toolruntime.Result{}, err
	}
	if err := rejectTableFormulas(tab); err != nil {
		return toolruntime.Result{}, err
	}
	switch strings.TrimSpace(p.Op) {
	case "filter":
		tab, err = dataprocess.Apply(ctx, tab, func(t dataprocess.Table) dataprocess.Table {
			return dataprocess.Filter(t, p.Column, p.Equals)
		})
	case "dedup":
		tab, err = dataprocess.Apply(ctx, tab, func(t dataprocess.Table) dataprocess.Table {
			return dataprocess.Dedup(t, p.Keys)
		})
	case "merge":
		otherRaw, readErr := readWorkspaceFile(root, p.OtherPath)
		if readErr != nil {
			return toolruntime.Result{}, readErr
		}
		other, otherErr := importTable(p.OtherPath, otherRaw)
		if otherErr != nil {
			return toolruntime.Result{}, otherErr
		}
		if err := rejectTableFormulas(other); err != nil {
			return toolruntime.Result{}, err
		}
		merged, mergeErr := dataprocess.Merge(tab, other)
		if mergeErr != nil {
			return toolruntime.Result{}, mergeErr
		}
		tab, err = dataprocess.Apply(ctx, merged, func(t dataprocess.Table) dataprocess.Table { return t })
	case "stats":
		if err := ctx.Err(); err != nil {
			return toolruntime.Result{}, err
		}
		st := dataprocess.Stats(tab, p.Column)
		body, _ := json.Marshal(map[string]any{"count": st.Count, "empty": st.Empty, "types": dataprocess.InferTypes(tab)})
		return toolruntime.Result{Output: string(body)}, nil
	case "export":
	default:
		return toolruntime.Result{}, errors.New("数据处理操作无效")
	}
	if err != nil {
		return toolruntime.Result{}, err
	}
	outPath := strings.TrimSpace(p.Out)
	if outPath == "" {
		outPath = strings.TrimSuffix(p.Path, filepath.Ext(p.Path)) + ".processed.csv"
	}
	if err := writeWorkspaceFile(root, outPath, dataprocess.ExportCSV(tab)); err != nil {
		return toolruntime.Result{}, err
	}
	body, _ := json.Marshal(map[string]any{"path": outPath, "rows": len(tab.Rows), "headers": tab.Headers})
	return toolruntime.Result{Output: string(body)}, nil
}

func executeImageBatch(root string, args json.RawMessage) (toolruntime.Result, error) {
	var p struct {
		Path   string `json:"path"`
		Op     string `json:"op"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Out    string `json:"out"`
	}
	if json.Unmarshal(args, &p) != nil {
		return toolruntime.Result{}, errors.New("图像处理参数无效")
	}
	if strings.EqualFold(filepath.Ext(p.Path), ".gif") {
		return toolruntime.Result{}, errors.New("不支持动画图像")
	}
	raw, err := readWorkspaceFile(root, p.Path)
	if err != nil {
		return toolruntime.Result{}, err
	}
	out, err := imagebatch.Process(raw, imagebatch.Op{Kind: p.Op, Width: p.Width, Height: p.Height})
	if err != nil {
		return toolruntime.Result{}, err
	}
	outPath := strings.TrimSpace(p.Out)
	if outPath == "" {
		outPath = strings.TrimSuffix(p.Path, filepath.Ext(p.Path)) + "." + strings.TrimSpace(p.Op) + ".png"
	}
	if err := writeWorkspaceFile(root, outPath, out); err != nil {
		return toolruntime.Result{}, err
	}
	body, _ := json.Marshal(map[string]any{"path": outPath, "bytes": len(out)})
	return toolruntime.Result{Output: string(body)}, nil
}

func executePDFCopy(root string, args json.RawMessage) (toolruntime.Result, error) {
	var p struct {
		Op    string   `json:"op"`
		Path  string   `json:"path"`
		Files []string `json:"files"`
		Out   string   `json:"out"`
	}
	if json.Unmarshal(args, &p) != nil {
		return toolruntime.Result{}, errors.New("PDF 操作参数无效")
	}
	var files []string
	switch strings.TrimSpace(p.Op) {
	case "split":
		raw, err := readWorkspaceFile(root, p.Path)
		if err != nil {
			return toolruntime.Result{}, err
		}
		parts, err := doctext.SplitPDFCopies(raw)
		if err != nil {
			return toolruntime.Result{}, err
		}
		stem := strings.TrimSuffix(p.Path, filepath.Ext(p.Path))
		if stem == "" {
			stem = "page"
		}
		for i, part := range parts {
			name := stem + "-p" + strconv.Itoa(i+1) + ".pdf"
			if err := writeWorkspaceFile(root, name, part); err != nil {
				return toolruntime.Result{}, err
			}
			files = append(files, name)
		}
	case "merge":
		var parts [][]byte
		for _, rel := range p.Files {
			raw, err := readWorkspaceFile(root, rel)
			if err != nil {
				return toolruntime.Result{}, err
			}
			parts = append(parts, raw)
		}
		if len(parts) == 0 {
			return toolruntime.Result{}, errors.New("没有可合并的 PDF")
		}
		merged, err := doctext.MergePDFCopies(parts)
		if err != nil {
			return toolruntime.Result{}, err
		}
		outPath := strings.TrimSpace(p.Out)
		if outPath == "" {
			outPath = "merged.pdf"
		}
		if err := writeWorkspaceFile(root, outPath, merged); err != nil {
			return toolruntime.Result{}, err
		}
		files = []string{outPath}
	default:
		return toolruntime.Result{}, errors.New("未知 PDF 操作")
	}
	body, _ := json.Marshal(map[string]any{
		"lossy":   doctext.PDFCopyLossy,
		"method":  doctext.PDFCopyMethod,
		"files":   files,
		"notice":  "仅保留可提取文本，版式与图片会丢失；表单或签名文件已拒绝。",
	})
	return toolruntime.Result{Output: string(body)}, nil
}

func importTable(path string, raw []byte) (dataprocess.Table, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return dataprocess.ImportJSON(raw)
	case ".xlsx":
		return dataprocess.ImportXLSX(raw)
	default:
		return dataprocess.ImportCSV(raw)
	}
}

func rejectTableFormulas(tab dataprocess.Table) error {
	for _, h := range tab.Headers {
		if err := dataprocess.RejectFormulaCell(h); err != nil {
			return err
		}
	}
	for _, row := range tab.Rows {
		for _, cell := range row {
			if err := dataprocess.RejectFormulaCell(cell); err != nil {
				return err
			}
		}
	}
	return nil
}

func readWorkspaceFile(root, rel string) ([]byte, error) {
	path, err := confinedWorkspacePath(root, rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func writeWorkspaceFile(root, rel string, data []byte) error {
	path, err := confinedWorkspacePath(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func confinedWorkspacePath(root, rel string) (string, error) {
	if err := workspace.ValidateRelPath(rel); err != nil {
		return "", errors.New("路径无效")
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}
