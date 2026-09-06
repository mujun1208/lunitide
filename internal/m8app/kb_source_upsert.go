package m8app

import (
	"context"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/oklog/ulid/v2"
	"path/filepath"
)

func (s *KBService) replayLocalSource(ctx context.Context, in KBUpsertInput) (*KBUpsertResult, error) {
	if !filepath.IsAbs(in.ContentRef) {
		return nil, nil
	}
	_, key, err := sourcePath(in.ContentRef)
	if err != nil {
		return nil, err
	}
	var result *KBUpsertResult
	err = s.uow.TransactKB(ctx, func(tx KBTx) error {
		st, ok := tx.(KBSourceTx)
		if !ok {
			return nil
		}
		if owner, ok := tx.(interface {
			KBCollectionOwned(string, string) (bool, error)
		}); ok {
			owned, e := owner.KBCollectionOwned(in.CollectionID, s.subject)
			if e != nil {
				return e
			}
			if !owned {
				return ErrKBCollectionNotFound
			}
		}
		source, ok, e := st.GetKBSource(in.CollectionID, key)
		if e != nil {
			return e
		}
		if !ok || source.State != "fresh" || source.SHA256 != in.SHA256 {
			return nil
		}
		if source.SourceLocator != in.SourceLocator || source.MediaType != in.MediaType {
			return ErrKBVersionConflict
		}
		refs, e := st.ListKBSourceDocuments(source.SourceID, source.Version)
		if e != nil {
			return e
		}
		if len(refs) != 1 {
			return ErrKBVersionConflict
		}
		result = &KBUpsertResult{DocumentID: refs[0].DocumentID, Version: refs[0].DocumentVersion, IndexState: m8core.KBIndexReady}
		return nil
	})
	if err != nil || result == nil {
		return result, err
	}
	usable, err := s.sourceDocumentUsable(ctx, m8core.KBDocument{DocumentID: result.DocumentID, Version: result.Version, ContentRef: in.ContentRef, SHA256: in.SHA256})
	if err != nil {
		return nil, err
	}
	if !usable {
		return nil, ErrKBDocumentNotReady
	}
	return result, nil
}

// registerDocumentSource also covers the original single-document bridge.
// Grouped sources must be refreshed through ingest; a single-document write
// cannot supersede just one group and expose an incomplete file generation.
func registerDocumentSource(tx KBTx, doc m8core.KBDocument, reason string) error {
	if !filepath.IsAbs(doc.ContentRef) {
		return nil
	}
	st, ok := tx.(KBSourceTx)
	if !ok {
		return nil
	}
	path, key, err := sourcePath(doc.ContentRef)
	if err != nil {
		return err
	}
	source, exists, err := st.GetKBSource(doc.CollectionID, key)
	if err != nil {
		return err
	}
	expected := source.Revision
	if exists {
		refs, e := st.ListKBSourceDocuments(source.SourceID, source.Version)
		if e != nil {
			return e
		}
		if source.State == "refreshing" || len(refs) != 1 || refs[0].DocumentID != doc.DocumentID {
			return ErrKBVersionConflict
		}
	} else {
		source = KBSource{SourceID: ulid.Make().String(), CollectionID: doc.CollectionID, Path: path, PathKey: key, CreatedAt: doc.CreatedAt}
	}
	source.Version++
	source.Revision++
	source.MediaType = doc.MediaType
	source.SourceLocator = doc.SourceLocator
	source.SHA256 = doc.SHA256
	source.CheckedAt = doc.CreatedAt
	source.Error = reason
	source.State = "fresh"
	if doc.IndexState != m8core.KBIndexReady {
		source.State = "failed"
	}
	if err = st.PutKBSource(source, expected); err != nil {
		return err
	}
	if err = st.PutKBSourceVersion(KBSourceVersion{source.SourceID, source.Version, source.SHA256, source.State, reason, doc.CreatedAt}); err != nil {
		return err
	}
	return st.PutKBSourceDocument(KBSourceDocument{source.SourceID, source.Version, 0, doc.DocumentID, doc.Version})
}
