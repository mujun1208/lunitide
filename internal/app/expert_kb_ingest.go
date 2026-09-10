package app

import (
	"context"

	"errors"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/m8app"
)

// ExpertKBIngest reads a local file into an expert collection with real bodies.
type ExpertKBIngest struct {
	kb *m8app.KBService
}

// NewExpertKBIngest wires a projector-capable KB service on the shared UoW.
func NewExpertKBIngest(uow m8app.KBUnitOfWork, subject string) *ExpertKBIngest {
	return &ExpertKBIngest{kb: m8app.NewKBService(uow, subject)}
}

// ExpertKBIngestInput is one file ingest command.
type ExpertKBIngestInput struct {
	ExpertID         string
	Path             string
	SourceLocator    string
	MediaType        string
	ExpectedRevision *int64
}

// ExpertKBIngestResult lists the document versions written for one file.
type ExpertKBIngestResult struct {
	CollectionID string
	Documents    []m8app.KBUpsertResult
	Source       m8app.KBSource
}

// Ingest parses a local file into searchable chunks. Markdown/plain files
// split directly; DOCX/PPTX/XLSX/PDF are decoded to their text layer first
// (doctext). A binary with no recoverable text is parked as a failed document
// carrying an honest reason instead of ingesting garbage bytes. Files that
// exceed MaxKBChunksPerVersion become multiple document_ids.
func (in *ExpertKBIngest) Ingest(ctx context.Context, input ExpertKBIngestInput) (ExpertKBIngestResult, error) {
	if in == nil || in.kb == nil {
		return ExpertKBIngestResult{}, m8app.ErrServiceUnavailable
	}
	coll, err := in.kb.EnsureExpertCollection(ctx, input.ExpertID)
	if err != nil {
		return ExpertKBIngestResult{}, err
	}
	result, err := in.kb.IngestLocalSource(ctx, m8app.KBLocalSourceInput{CollectionID: coll.CollectionID, Path: input.Path, MediaType: input.MediaType, SourceLocator: input.SourceLocator, ExpectedRevision: input.ExpectedRevision})
	return ExpertKBIngestResult{CollectionID: result.CollectionID, Documents: result.Documents, Source: result.Source}, err
}

var errOCRCoverageIncomplete = errors.New("文档识别覆盖不完整，未入库")

// ingestFailReason turns a doctext error into an operator-facing reason.
func ingestFailReason(err error) string {
	switch {
	case errors.Is(err, doctext.ErrNoTextLayer):
		return "无法抽取正文（可能是扫描件或空文档）"
	case errors.Is(err, doctext.ErrUnsupportedFormat):
		return "暂不支持该文件格式的正文解析"
	case errors.Is(err, doctext.ErrBudgetExceeded):
		return "文档超过解析上限"
	case errors.Is(err, errOCRCoverageIncomplete):
		return errOCRCoverageIncomplete.Error()
	default:
		return localizeStoredKBFailReason(err.Error())
	}
}
