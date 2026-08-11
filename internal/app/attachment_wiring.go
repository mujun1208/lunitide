package app

import (
	"github.com/lunitide/lunitide/internal/attachmentapp"
)

// AttachmentStore combines the storage interface needed by the attachment
// service. A single Store implementation typically satisfies this.
type AttachmentStore interface {
	attachmentapp.Store
}

// SetupAttachmentService wires the attachment service into the engine.
// The store must satisfy AttachmentStore. The fileStorage handles controlled
// file I/O within the secure data directory. When either is nil, the method
// is a no-op (ADR-005 §7).
func (e *Engine) SetupAttachmentService(store AttachmentStore, fileStorage attachmentapp.FileStorage) {
	if store == nil {
		return
	}
	e.SetAttachmentService(attachmentapp.NewService(store, fileStorage))
}
