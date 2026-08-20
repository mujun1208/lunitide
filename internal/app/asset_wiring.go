package app

import "github.com/lunitide/lunitide/internal/attachmentapp"

// SetAssetStorage wires the asset template store into the engine.
func (e *Engine) SetAssetStorage(store AssetTemplateStore) { e.assets = store }

// SetDeliverableStorage wires the project deliverable store into the engine.
func (e *Engine) SetDeliverableStorage(store DeliverableStore) { e.deliverables = store }

// SetProjectAttachmentStorage wires project phase attachment storage and file I/O.
func (e *Engine) SetProjectAttachmentStorage(store ProjectAttachmentStore, files attachmentapp.FileStorage) {
	e.projectAttachments = store
	e.projectAttachmentFiles = files
}
