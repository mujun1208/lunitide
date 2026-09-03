package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/m8core"
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
	ExpertID      string
	Path          string
	SourceLocator string
	MediaType     string
}

// ExpertKBIngestResult lists the document versions written for one file.
type ExpertKBIngestResult struct {
	CollectionID string
	Documents    []m8app.KBUpsertResult
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
	path := strings.TrimSpace(input.Path)
	if !filepath.IsAbs(path) {
		return ExpertKBIngestResult{}, m8app.ErrPayloadInvalid
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ExpertKBIngestResult{}, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	locator := strings.TrimSpace(input.SourceLocator)
	if locator == "" {
		locator = path
	}
	coll, err := in.kb.EnsureExpertCollection(ctx, input.ExpertID)
	if err != nil {
		return ExpertKBIngestResult{}, err
	}
	out := ExpertKBIngestResult{CollectionID: coll.CollectionID}

	extracted, xerr := doctext.Extract(path, raw, input.MediaType)
	if xerr != nil {
		// Record a failed version so the UI shows the document with an honest
		// reason (scanned/empty/unsupported) rather than silently dropping it.
		res, uerr := in.kb.UpsertDocument(ctx, m8app.KBUpsertInput{
			CollectionID:  coll.CollectionID,
			DocumentID:    ulid.Make().String(),
			MediaType:     failMediaType(path, input.MediaType),
			ContentRef:    path,
			SHA256:        digest,
			SourceLocator: locator,
			Projector: func(context.Context, m8core.KBDocument) ([]m8core.KBChunk, error) {
				return nil, fmt.Errorf("%w: %s", m8app.ErrKBIndexFailed, ingestFailReason(xerr))
			},
		})
		out.Documents = append(out.Documents, res)
		return out, uerr
	}

	parts := m8app.SplitSearchableParts(extracted.Media, strings.TrimSpace(extracted.Text))
	if len(parts) == 0 {
		parts = []string{strings.TrimSpace(extracted.Text)}
	}
	for _, group := range groupParts(parts, m8core.MaxKBChunksPerVersion) {
		group := group
		res, uerr := in.kb.UpsertDocument(ctx, m8app.KBUpsertInput{
			CollectionID:  coll.CollectionID,
			DocumentID:    ulid.Make().String(),
			MediaType:     extracted.Media,
			ContentRef:    path,
			SHA256:        digest,
			SourceLocator: locator,
			Projector: func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error) {
				return m8app.ChunksFromParts(doc, group)
			},
		})
		out.Documents = append(out.Documents, res)
		if uerr != nil {
			return out, uerr
		}
	}
	return out, nil
}

// ingestFailReason turns a doctext error into an operator-facing reason.
func ingestFailReason(err error) string {
	switch {
	case errors.Is(err, doctext.ErrNoTextLayer):
		return "无法抽取正文（可能是扫描件或空文档）"
	case errors.Is(err, doctext.ErrUnsupportedFormat):
		return "暂不支持该文件格式的正文解析"
	default:
		return err.Error()
	}
}

// failMediaType keeps a non-empty media type on a failed record (UpsertDocument
// requires one) while preserving the caller's hint when present.
func failMediaType(path, declared string) string {
	if m := strings.TrimSpace(declared); m != "" {
		return m
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
}

func groupParts(parts []string, n int) [][]string {
	if n < 1 {
		n = m8core.MaxKBChunksPerVersion
	}
	var groups [][]string
	for len(parts) > 0 {
		end := n
		if end > len(parts) {
			end = len(parts)
		}
		groups = append(groups, parts[:end])
		parts = parts[end:]
	}
	return groups
}
