package app

// SetAssetStorage wires the asset template store into the engine.
func (e *Engine) SetAssetStorage(store AssetTemplateStore) { e.assets = store }

// SetDeliverableStorage wires the project deliverable store into the engine.
func (e *Engine) SetDeliverableStorage(store DeliverableStore) { e.deliverables = store }
