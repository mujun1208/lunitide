package officestudio

import (
	"crypto/sha256"
	"encoding/hex"
)

type SameSourcePDF struct {
	SourceKind   Kind   `json:"sourceKind"`
	SourceSHA256 string `json:"sourceSha256"`
	PDFSHA256    string `json:"pdfSha256"`
	Stale        bool   `json:"stale"`
	Notice       string `json:"notice"`
}

func BindSameSourcePDF(kind Kind, sourceSHA256 string, pdf []byte) SameSourcePDF {
	sum := sha256.Sum256(pdf)
	return SameSourcePDF{
		SourceKind:   kind,
		SourceSHA256: sourceSHA256,
		PDFSHA256:    hex.EncodeToString(sum[:]),
		Notice:       "同源 PDF 绑定对应 Office 版本摘要；源文件再编辑后需重建。",
	}
}

func InvalidateSameSourcePDF(bind SameSourcePDF, currentSourceSHA256 string) SameSourcePDF {
	if bind.SourceSHA256 != currentSourceSHA256 {
		bind.Stale = true
		bind.Notice = "源文件已变化，同源 PDF 已失效，需按新版本重建。"
	}
	return bind
}

func (b SameSourcePDF) ValidFor(sourceSHA256 string) bool {
	return !b.Stale && b.SourceSHA256 == sourceSHA256 && b.PDFSHA256 != ""
}

func AssessSameSourcePDF(bind SameSourcePDF, currentSourceSHA256 string, pdf []byte) Check {
	bind = InvalidateSameSourcePDF(bind, currentSourceSHA256)
	if !bind.ValidFor(currentSourceSHA256) {
		status := "failed"
		if bind.PDFSHA256 == "" {
			status = "missing"
		}
		return Check{ID: "same-source", Status: status, Message: bind.Notice}
	}
	if bind.SourceKind != DOCX {
		return Check{ID: "same-source", Status: "failed", Message: "同源 PDF 绑定 DOCX 源版本；独立 PDF 只承诺已支持的 title/body。"}
	}
	for _, c := range PDFDeliveryChecks(pdf) {
		if c.ID == "pdf-parse" || c.ID == "page-coverage" {
			if c.Status != "passed" {
				return Check{ID: "same-source", Status: "failed", Message: "同源 PDF 页树无效，不能绑定当前版本"}
			}
		}
	}
	return Check{ID: "same-source", Status: "passed", Message: "同源 PDF 绑定当前 Office 版本摘要"}
}
