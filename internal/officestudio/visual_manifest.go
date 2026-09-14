package officestudio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	VisualStdoutMaxBytes = 1 << 20
	VisualStderrMaxBytes = 64 << 10
)

type VisualManifest struct {
	SchemaVersion   int          `json:"schemaVersion"`
	SourceSHA       string       `json:"sourceSHA,omitempty"`
	Renderer        string       `json:"renderer,omitempty"`
	RendererVersion string       `json:"rendererVersion,omitempty"`
	PageCount       int          `json:"pageCount"`
	Pages           []VisualPage `json:"pages,omitempty"`
	PageDigests     []string     `json:"pageDigests,omitempty"`
}

type VisualPage struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type VisualReviewResult struct {
	Reviewer      string              `json:"reviewer"`
	Version       string              `json:"version"`
	SourceSHA     string              `json:"sourceSHA"`
	ReviewedPages []int               `json:"reviewedPages"`
	Issues        []visualReviewIssue `json:"issues"`
}

type visualReviewIssue struct {
	Page     int       `json:"page"`
	NodeID   string    `json:"nodeId"`
	Code     string    `json:"code"`
	Severity string    `json:"severity"`
	Message  string    `json:"message"`
	BBox     []float64 `json:"bbox,omitempty"`
}

func visualPageFileName(i int) string {
	return fmt.Sprintf("page-%03d.bin", i)
}

func ValidateVisualManifest(man VisualManifest, expectedPages int) error {
	if expectedPages < 1 {
		return fmt.Errorf("visual: expected page count required")
	}
	if man.PageCount != expectedPages {
		return fmt.Errorf("visual: pageCount %d != expected %d", man.PageCount, expectedPages)
	}
	if len(man.PageDigests) != expectedPages {
		return fmt.Errorf("visual: digest count %d != %d", len(man.PageDigests), expectedPages)
	}
	for i, d := range man.PageDigests {
		if strings.TrimSpace(d) == "" {
			return fmt.Errorf("visual: empty digest at %d", i)
		}
	}
	if len(man.Pages) == 0 {
		return nil
	}
	if len(man.Pages) != expectedPages {
		return fmt.Errorf("visual: pages %d != %d", len(man.Pages), expectedPages)
	}
	seen := map[int]string{}
	for _, p := range man.Pages {
		if p.Index < 0 || p.Index >= expectedPages {
			return fmt.Errorf("visual: page index %d out of range", p.Index)
		}
		if prev, ok := seen[p.Index]; ok && prev != p.SHA256 {
			return fmt.Errorf("visual: conflicting digest for page %d", p.Index)
		}
		seen[p.Index] = p.SHA256
		if p.SHA256 != man.PageDigests[p.Index] {
			return fmt.Errorf("visual: digest mismatch page %d", p.Index)
		}
	}
	for i := 0; i < expectedPages; i++ {
		if _, ok := seen[i]; !ok {
			return fmt.Errorf("visual: missing page %d", i)
		}
	}
	return nil
}

func ValidateVisualProcess(exitCode int, stdout string, stderr []byte) error {
	_ = stderr
	if len(stdout) > VisualStdoutMaxBytes {
		return fmt.Errorf("visual: stdout exceeds 1 MiB")
	}
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return fmt.Errorf("visual: exit %d with empty JSON", exitCode)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(stdout), &obj); err != nil || obj == nil {
		return fmt.Errorf("visual: stdout is not a JSON object")
	}
	if exitCode != 0 {
		return fmt.Errorf("visual: process exit %d", exitCode)
	}
	return nil
}

func validateReviewedPages(expected int, reviewed []int) error {
	if expected < 1 {
		return fmt.Errorf("visual: expected page count required")
	}
	seen := map[int]bool{}
	for _, p := range reviewed {
		if p < 0 || p >= expected {
			return fmt.Errorf("visual: reviewed page %d out of range", p)
		}
		seen[p] = true
	}
	for i := 0; i < expected; i++ {
		if !seen[i] {
			return fmt.Errorf("visual: reviewedPages missing %d", i)
		}
	}
	return nil
}

func ParseVisualReview(stdout string, expectedPages int) (VisualReviewResult, error) {
	if err := ValidateVisualProcess(0, stdout, nil); err != nil {
		return VisualReviewResult{}, err
	}
	var result VisualReviewResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &result); err != nil {
		return VisualReviewResult{}, err
	}
	if err := validateReviewedPages(expectedPages, result.ReviewedPages); err != nil {
		return result, err
	}
	return result, nil
}

func VisualPageCoverageCheck(expected int, reviewed []int) Check {
	if err := validateReviewedPages(expected, reviewed); err != nil {
		return Check{ID: "page-coverage", Status: "failed", Message: "视觉结果漏页，拒绝全量标记"}
	}
	return Check{ID: "page-coverage", Status: "passed", Message: fmt.Sprintf("视觉页覆盖 0..%d；规则分仍未校准，不能当作高端商用", expected-1)}
}

func WriteVisualReviewWorkspace(dir string, pages [][]byte) (VisualManifest, error) {
	man := VisualManifest{SchemaVersion: 2, PageCount: len(pages)}
	for i, page := range pages {
		name := visualPageFileName(i)
		if err := os.WriteFile(filepath.Join(dir, name), page, 0600); err != nil {
			return man, err
		}
		sum := sha256.Sum256(page)
		hex := hex.EncodeToString(sum[:])
		man.PageDigests = append(man.PageDigests, hex)
		man.Pages = append(man.Pages, VisualPage{Index: i, Path: name, SHA256: hex})
	}
	if err := ValidateVisualManifest(man, len(pages)); err != nil {
		return man, err
	}
	raw, err := json.Marshal(man)
	if err != nil {
		return man, err
	}
	return man, os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600)
}

type PPTEditableStats struct {
	Slides             int
	Texts              int
	Charts             int
	Shapes             int
	RelationsOK        bool
	VisualFullCoverage bool
}

func PPTEditableInventory(data []byte) (PPTEditableStats, error) {
	var out PPTEditableStats
	insp, err := Inspect(PPTX, data)
	if err != nil {
		return out, err
	}
	p, err := readPackage(data)
	if err != nil {
		return out, err
	}
	for name := range p.parts {
		if isSlideContentPart(name) {
			out.Slides++
		}
	}
	for _, n := range insp.Nodes {
		switch {
		case n.Kind == "chart" || n.Chart != nil:
			out.Charts++
		case n.Kind == "text" || strings.TrimSpace(n.Text) != "":
			out.Texts++
		}
	}
	for name, body := range p.parts {
		if !isSlideContentPart(name) {
			continue
		}
		out.Shapes += strings.Count(string(body), "<p:sp>")
	}
	out.RelationsOK = true
	if out.Charts > 0 {
		found := false
		for name, body := range p.parts {
			if !strings.Contains(name, "/slides/_rels/") {
				continue
			}
			if strings.Contains(string(body), "relationships/chart") {
				found = true
				break
			}
		}
		out.RelationsOK = found
	}
	return out, nil
}
