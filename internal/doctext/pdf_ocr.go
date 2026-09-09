package doctext

type OCRPage struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}
type PDFOCRResult struct {
	Method   string    `json:"method"`
	Language string    `json:"language"`
	Pages    []OCRPage `json:"pages"`
}
