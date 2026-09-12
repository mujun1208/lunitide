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
