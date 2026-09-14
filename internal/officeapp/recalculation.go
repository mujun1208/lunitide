package officeapp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lunitide/lunitide/internal/officerender"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

type RecalculationReceipt struct {
	VersionSHA    string `json:"versionSha"`
	FormulaDigest string `json:"formulaDigest,omitempty"`
	CachedValue   string `json:"cachedValue,omitempty"`
	OracleValue   string `json:"oracleValue,omitempty"`
	Supported     bool   `json:"supported"`
	Status        string `json:"status"`
	Notice        string `json:"notice,omitempty"`
}

var formulaFuncName = regexp.MustCompile(`(?i)\b([A-Z][A-Z0-9.]*)\s*\(`)

func (s *Service) RecalculateWorkbook(ctx context.Context, taskID, versionID string) (RecalculationReceipt, error) {
	v, b, err := s.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return RecalculationReceipt{}, err
	}
	if v.Kind != "xlsx" {
		return RecalculationReceipt{}, errors.New("重算仅支持工作簿")
	}
	i, err := content.Inspect(content.XLSX, b)
	if err != nil {
		return RecalculationReceipt{}, err
	}
	rec := RecalculationReceipt{VersionSHA: v.SHA256, Status: "unknown", Notice: "尚未由隔离 LibreOffice 完成重算。"}
	for _, n := range i.Nodes {
		if n.Kind != "cell:formula" {
			continue
		}
		if rec.FormulaDigest == "" {
			rec.FormulaDigest = n.Digest
		}
		if !supportedExcelFormula(n.Text) {
			rec.Supported = false
			rec.Status = "unknown"
			rec.Notice = "不支持的函数，要求可计算的正式交付保持 unknown。"
			return rec, nil
		}
	}
	rec.Supported = true
	if cents, err := content.IndependentIntegerCents(i); err == nil {
		rec.OracleValue = strconv.FormatInt(cents, 10)
	} else if sum, err := content.IndependentRangeSum(i, "B2:B5"); err == nil {
		rec.OracleValue = strconv.FormatInt(sum, 10)
	}
	if rec.OracleValue != "" {
		if n, err := strconv.ParseInt(rec.OracleValue, 10, 64); err == nil && content.FormulaCacheDisagreesWithOracle(b, n) {
			rec.Status = "mismatch"
			rec.Notice = "缓存值与独立判定不一致。"
			return rec, nil
		}
	}
	if s.Renderer != nil {
		_, renderErr := s.Renderer.RenderWithChecks(ctx, "xlsx", b, officerender.NativeOptions{Recalculate: true, ExportUpdatedCopy: true})
		if renderErr == nil {
			rec.Status = "unknown"
			rec.Notice = "渲染器回执未与独立判定同时核验，不能标为已匹配。"
			return rec, nil
		}
		if rec.Notice == "" {
			rec.Notice = fmt.Sprintf("隔离重算不可用：%v", renderErr)
		}
	}
	return rec, nil
}

func supportedExcelFormula(formula string) bool {
	formula = strings.TrimSpace(formula)
	if formula == "" {
		return false
	}
	for _, name := range formulaFuncName.FindAllStringSubmatch(formula, -1) {
		switch strings.ToUpper(name[1]) {
		case "SUM", "COUNTA", "IF", "ABS":
			continue
		default:
			return false
		}
	}
	return true
}
