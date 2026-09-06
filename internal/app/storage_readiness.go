package app

import "context"

type StorageReadiness interface{ CheckReadiness(context.Context) error }

func (e *Engine) SetStorageReadiness(check StorageReadiness) { e.storageReadiness = check }
