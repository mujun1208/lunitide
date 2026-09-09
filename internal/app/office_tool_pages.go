package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

// Model tool summaries are limited to 4096 bytes by the shared chat runtime.
// Keep complete JSON below that boundary; Bridge previews have a separate cap.
const officeToolPageLimit = 3800

func officeToolJSON(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(b) > officeToolPageLimit {
		return "", domain.ErrInvalid
	}
	return string(b), nil
}

func officeToolAppend(page map[string]any, key string, row any) bool {
	rows, _ := page[key].([]any)
	page[key] = append(rows, row)
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) > officeToolPageLimit-128 {
		page[key] = rows
		return false
	}
	return true
}

func officeToolSnippet(value string, limit int) string {
	r := []rune(value)
	return string(r[:min(len(r), limit)])
}
func officeToolSlice(value string, offset int) (string, int, int, error) {
	r := []rune(value)
	if offset < 0 || offset > len(r) {
		return "", 0, 0, domain.ErrInvalid
	}
	end := min(len(r), offset+280)
	return string(r[offset:end]), end, len(r), nil
}

func (e *Engine) officeInspectPage(ctx context.Context, taskID, versionID, view string, offset, nodeOffset, textOffset int) (string, error) {
	if offset < 0 || nodeOffset < 0 || textOffset < 0 {
		return "", domain.ErrInvalid
	}
	heads, err := e.officeStudio.Store.ListOfficeHeads(ctx, taskID)
	if err != nil {
		return "", err
	}
	headByArtifact := map[string]domain.Head{}
	for _, h := range heads {
		headByArtifact[h.ArtifactID] = h
	}
	if versionID == "" {
		versions, err := e.officeStudio.Store.ListOfficeVersions(ctx, taskID, "")
		if err != nil {
			return "", err
		}
		if offset > len(versions) || textOffset != 0 {
			return "", domain.ErrInvalid
		}
		page := map[string]any{"taskId": taskID, "versions": []any{}, "heads": []any{}, "offset": offset, "nextOffset": offset, "total": len(versions), "hasMore": false}
		for index := offset; index < len(versions); index++ {
			v := versions[index]
			h := headByArtifact[v.ArtifactID]
			row := map[string]any{"versionId": v.ID, "artifactId": v.ArtifactID, "name": officeToolSnippet(v.Name, 80), "nameTruncated": len([]rune(v.Name)) > 80, "kind": v.Kind, "quality": v.Quality, "versionNo": v.VersionNo, "sha256": v.SHA256, "headRevision": h.Revision, "latestVersionId": h.LatestVersionID}
			page["nextOffset"], page["hasMore"] = index+1, index+1 < len(versions)
			if !officeToolAppend(page, "versions", row) {
				page["nextOffset"], page["hasMore"] = index, true
				break
			}
		}
		return officeToolJSON(page)
	}
	v, data, err := e.officeStudio.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return "", err
	}
	i, err := content.Inspect(content.Kind(v.Kind), data)
	if err != nil {
		return "", err
	}
	if v.Kind == "pdf" && i.Editability == "blocked" && view != "parts" {
		return "", fmt.Errorf("PDF is encrypted or contains active content; provide a readable, inactive copy")
	}
	if view == "text" {
		source, err := e.officeVersionText(ctx, v, data)
		if err != nil {
			return "", err
		}
		return officeSourceTextPage(taskID, v, source, textOffset)
	}
	textMethod := ""
	if v.Kind == "pdf" && view != "parts" {
		if i.Editability == "blocked" {
			return "", fmt.Errorf("PDF is encrypted or contains active content; provide a readable, inactive copy")
		}
		extracted, extractErr := e.officeVersionText(ctx, v, data)
		if extractErr != nil {
			return "", fmt.Errorf("PDF text extraction failed (no source content was read): %w", extractErr)
		}
		textMethod = extracted.method
		text := []rune(extracted.text)
		for start := 0; start < len(text); start += 1200 {
			i.Nodes = append(i.Nodes, content.Node{ID: fmt.Sprintf("pdf-text-%d", start/1200), Kind: "text", Text: string(text[start:min(start+1200, len(text))]), Editable: false})
		}
	}
	h := headByArtifact[v.ArtifactID]
	page := map[string]any{"taskId": taskID, "versionId": v.ID, "artifactId": v.ArtifactID, "kind": v.Kind, "sha256": v.SHA256, "expectedRevision": h.Revision, "latestVersionId": h.LatestVersionID, "previewBasis": "structure", "notice": "Read all text chunks before replacing a node. Structural inspection does not verify rendered layout."}
	if textMethod == "windows-ocr" {
		page["method"] = textMethod
		page["notice"] = "Local OCR of rendered PDF pages; text/numbers may be incorrect or missing. Prefer a supplied editable original. Use view=text for reading, not editing."
	}
	if view == "parts" {
		if v.Kind == "pdf" {
			page["notice"] = "PDF is not an OOXML package. An empty parts list says nothing about visible page content or its text layer. Use view=text to read text or local OCR."
		}
		if offset > len(i.Parts) {
			return "", domain.ErrInvalid
		}
		page["parts"], page["offset"], page["nextOffset"], page["total"], page["nextTextOffset"] = []any{}, offset, offset, len(i.Parts), 0
		for index := offset; index < len(i.Parts); index++ {
			p := i.Parts[index]
			start := 0
			if index == offset {
				start = textOffset
			}
			name, end, total, err := officeToolSlice(p.Name, start)
			if err != nil {
				return "", err
			}
			row := map[string]any{"name": name, "nameOffset": start, "nextNameOffset": end, "nameRunes": total, "nameComplete": start == 0 && end == total, "sha256": p.SHA256, "size": p.Size}
			if !officeToolAppend(page, "parts", row) {
				break
			}
			if end < total {
				page["nextOffset"], page["nextTextOffset"] = index, end
				break
			}
			page["nextOffset"] = index + 1
		}
		page["hasMore"] = page["nextOffset"].(int) < len(i.Parts)
		return officeToolJSON(page)
	}
	if view != "" && view != "nodes" {
		return "", domain.ErrInvalid
	}
	if nodeOffset > len(i.Nodes) {
		return "", domain.ErrInvalid
	}
	partDigests := map[string]string{}
	for _, p := range i.Parts {
		partDigests[p.Name] = p.SHA256
	}
	page["nodes"], page["nodeOffset"], page["nextNodeOffset"], page["totalNodes"], page["nextTextOffset"] = []any{}, nodeOffset, nodeOffset, len(i.Nodes), 0
	for index := nodeOffset; index < len(i.Nodes); index++ {
		n := i.Nodes[index]
		start := 0
		if index == nodeOffset {
			start = textOffset
		}
		text, end, total, err := officeToolSlice(n.Text, start)
		if err != nil {
			return "", err
		}
		row := map[string]any{"id": n.ID, "digest": n.Digest, "kind": n.Kind, "editable": n.Editable, "text": text, "textOffset": start, "nextTextOffset": end, "textRunes": total, "textComplete": start == 0 && end == total, "partDigest": partDigests[n.Part]}
		if n.Image != nil {
			row["image"] = n.Image
		}
		if n.Chart != nil {
			row["chart"] = n.Chart
		}
		// Arbitrarily long imported names remain available through view=parts;
		// they must not crowd the stable edit ID/digest out of the tool frame.
		partWire, _ := json.Marshal(n.Part)
		if len(partWire) <= 320 {
			row["part"] = n.Part
		} else {
			row["partNameOmitted"] = true
		}
		locatorWire, _ := json.Marshal(n.Locator)
		if len(locatorWire) <= 200 {
			row["locator"] = n.Locator
		}
		if !officeToolAppend(page, "nodes", row) {
			break
		}
		if end < total {
			page["nextNodeOffset"], page["nextTextOffset"] = index, end
			break
		}
		page["nextNodeOffset"] = index + 1
	}
	page["hasMore"] = page["nextNodeOffset"].(int) < len(i.Nodes)
	return officeToolJSON(page)
}

func officeMetricRow(m domain.Metric, textOffset int) (map[string]any, error) {
	raw, end, total, err := officeToolSlice(m.RawValue, textOffset)
	if err != nil {
		return nil, err
	}
	if end-textOffset > 100 {
		end = textOffset + 100
		raw = string([]rune(m.RawValue)[textOffset:end])
	}
	row := map[string]any{"id": m.ID, "name": officeToolSnippet(m.Name, 80), "sourceVersionId": m.SourceVersionID, "sourceSha256": m.SourceSHA256, "sourceNodeId": m.SourceNodeID, "sourceNodeDigest": m.SourceNodeDigest, "valueType": m.ValueType, "rawValue": raw, "textOffset": textOffset, "nextTextOffset": end, "textRunes": total, "textComplete": textOffset == 0 && end == total, "unit": m.Unit, "currency": m.Currency, "period": m.Period, "roundingPolicy": m.RoundingPolicy}
	if m.DisplayValue != m.RawValue {
		row["displayValue"] = officeToolSnippet(m.DisplayValue, 100)
		row["displayValueTruncated"] = len([]rune(m.DisplayValue)) > 100
	}
	return row, nil
}

func officeBundleRow(b domain.Bundle) map[string]any {
	return map[string]any{"id": b.ID, "title": officeToolSnippet(b.Title, 80), "fileCount": len(b.Files), "schemaVersion": b.SchemaVersion}
}

func officeDeliveryPage(taskID, action, metricID, bundleID string, offset, textOffset, fileOffset int, result any) (string, error) {
	if offset < 0 || textOffset < 0 || fileOffset < 0 {
		return "", domain.ErrInvalid
	}
	page := map[string]any{"taskId": taskID, "action": action}
	switch value := result.(type) {
	case domain.Metric:
		row, err := officeMetricRow(value, 0)
		if err != nil {
			return "", err
		}
		page["metric"] = row
		page["notice"] = "Use action=metrics with metricId and nextTextOffset to read any remaining value."
	case []domain.Metric:
		if metricID != "" {
			found := false
			for _, m := range value {
				if m.ID == metricID {
					value = []domain.Metric{m}
					found = true
					break
				}
			}
			if !found {
				return "", domain.ErrNotFound
			}
		}
		if offset > len(value) {
			return "", domain.ErrInvalid
		}
		page["metrics"], page["offset"], page["nextOffset"], page["total"], page["nextTextOffset"] = []any{}, offset, offset, len(value), 0
		for index := offset; index < len(value); index++ {
			start := 0
			if index == offset {
				start = textOffset
			}
			row, err := officeMetricRow(value[index], start)
			if err != nil {
				return "", err
			}
			if !officeToolAppend(page, "metrics", row) {
				break
			}
			if row["nextTextOffset"].(int) < row["textRunes"].(int) {
				page["nextOffset"], page["nextTextOffset"] = index, row["nextTextOffset"]
				break
			}
			page["nextOffset"] = index + 1
		}
		page["hasMore"] = page["nextOffset"].(int) < len(value)
	case domain.Bundle:
		page["bundle"] = officeBundleRow(value)
		page["notice"] = "Use action=bundles with bundleId and fileOffset to inspect every fixed-version file."
	case []domain.Bundle:
		if bundleID != "" {
			var selected *domain.Bundle
			for _, b := range value {
				if b.ID == bundleID {
					copy := b
					selected = &copy
					break
				}
			}
			if selected == nil {
				return "", domain.ErrNotFound
			}
			if fileOffset > len(selected.Files) {
				return "", domain.ErrInvalid
			}
			page["bundle"], page["files"], page["fileOffset"], page["nextFileOffset"], page["totalFiles"] = officeBundleRow(*selected), []any{}, fileOffset, fileOffset, len(selected.Files)
			for index := fileOffset; index < len(selected.Files); index++ {
				f := selected.Files[index]
				row := map[string]any{"versionId": f.VersionID, "artifactId": f.ArtifactID, "name": officeToolSnippet(f.Name, 80), "nameTruncated": len([]rune(f.Name)) > 80, "kind": f.Kind, "sha256": f.SHA256, "size": f.Size, "quality": f.Quality, "accepted": f.Accepted}
				if !officeToolAppend(page, "files", row) {
					break
				}
				page["nextFileOffset"] = index + 1
			}
			page["hasMore"] = page["nextFileOffset"].(int) < len(selected.Files)
		} else {
			if offset > len(value) {
				return "", domain.ErrInvalid
			}
			page["bundles"], page["offset"], page["nextOffset"], page["total"] = []any{}, offset, offset, len(value)
			for index := offset; index < len(value); index++ {
				if !officeToolAppend(page, "bundles", officeBundleRow(value[index])) {
					break
				}
				page["nextOffset"] = index + 1
			}
			page["hasMore"] = page["nextOffset"].(int) < len(value)
		}
	default:
		return "", domain.ErrInvalid
	}
	return officeToolJSON(page)
}

func officeToolAudit(v domain.Version) string {
	var audit struct {
		ChangedParts []string                   `json:"changedParts"`
		Calculation  *content.CalculationReport `json:"calculation"`
	}
	if json.Unmarshal(v.Spec, &audit) != nil || len(audit.ChangedParts) == 0 && audit.Calculation == nil {
		return ""
	}
	row := map[string]any{"changedPartCount": len(audit.ChangedParts)}
	if audit.Calculation != nil {
		b, _ := json.Marshal(audit.Calculation)
		var c map[string]any
		_ = json.Unmarshal(b, &c)
		delete(c, "affectedFormulaNodes")
		delete(c, "warnings")
		delete(c, "unsupportedFeatures")
		for key, value := range c {
			if text, ok := value.(string); ok {
				c[key] = officeToolSnippet(text, 160)
			}
		}
		row["calculation"] = c
		row["affectedFormulaNodeCount"] = len(audit.Calculation.AffectedFormulaNodes)
	}
	b, _ := json.Marshal(row)
	if len(b) > 1600 {
		return `{"notice":"Detailed calculation coverage is retained with the file; inspect it before claiming a complete recalculation."}`
	}
	return strings.TrimSpace(string(b))
}
