package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// SetAssetStorage wires the asset template store into the engine.
func (e *Engine) SetAssetStorage(store AssetTemplateStore) {
	e.assets = store
	e.useEnabledDeckTemplates()
}

// SetDeliverableStorage wires the project deliverable store into the engine.
func (e *Engine) SetDeliverableStorage(store DeliverableStore) { e.deliverables = store }

// SetTemplateFileStorage wires org-level template file I/O.
func (e *Engine) SetTemplateFileStorage(files attachmentapp.FileStorage) {
	e.templateFiles = files
	e.useEnabledDeckTemplates()
}

// useEnabledDeckTemplates applies an enabled PPT template from the asset
// library when pptx.gen writes a deck. Draft rows are skipped. If none can be
// filled, generation falls back to the designed deck.
func (e *Engine) useEnabledDeckTemplates() {
	toolruntime.EnabledDeckTemplate = func() ([]byte, bool) {
		if e == nil || !assetStoreAvailable(e.assets) || e.templateFiles == nil {
			return nil, false
		}
		items, err := e.assets.ListAssetTemplates(context.Background(), asset.Filter{
			Status:       asset.StatusEnabled,
			TemplateType: asset.TemplateTypePPT,
			Limit:        8,
		})
		if err != nil {
			return nil, false
		}
		for _, item := range items {
			if item.Status != asset.StatusEnabled || item.TemplateType != asset.TemplateTypePPT || item.FilePath == "" {
				continue
			}
			raw, readErr := e.templateFiles.ReadFile(context.Background(), item.FilePath)
			if readErr != nil || len(raw) == 0 {
				continue
			}
			return raw, true
		}
		return nil, false
	}
}

// SetProjectAttachmentStorage wires project phase attachment storage and file I/O.
func (e *Engine) SetProjectAttachmentStorage(store ProjectAttachmentStore, files attachmentapp.FileStorage) {
	e.projectAttachments = store
	e.projectAttachmentFiles = files
}
