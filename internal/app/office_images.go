package app

import (
	"context"
	"fmt"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

// Image bytes come from the task's verified attachment store, never a model
// supplied path, URL or inline executable payload. The content layer performs
// the independent PNG/JPEG and pixel-size validation.
func (e *Engine) officeImageSource(ctx context.Context, sessionID, sourceID, sha string) ([]byte, error) {
	if e.attachmentService == nil || !validCanonicalULID(sourceID) || len(sha) != 64 {
		return nil, domain.ErrInvalid
	}
	a, data, err := e.attachmentService.ReadOfficeSnapshot(ctx, sourceID, sessionID)
	if err != nil {
		return nil, err
	}
	if a.SHA256 != strings.ToLower(sha) {
		return nil, fmt.Errorf("%w: 图片附件已变化，请重新选择", domain.ErrConflict)
	}
	if len(data) > 8<<20 {
		return nil, fmt.Errorf("%w: 图片不能超过 8 MiB", domain.ErrInvalid)
	}
	return data, nil
}

func (e *Engine) hydrateOfficeImages(ctx context.Context, sessionID string, spec *content.Spec) error {
	count := 0
	totalBytes := 0
	cache := make(map[string][]byte)
	for si := range spec.Slides {
		if len(spec.Slides[si].Images) > 16 {
			return domain.ErrInvalid
		}
		for ii := range spec.Slides[si].Images {
			count++
			if count > 128 {
				return domain.ErrInvalid
			}
			img := &spec.Slides[si].Images[ii]
			key := img.SourceID + ":" + img.SHA256
			data, ok := cache[key]
			if !ok {
				var err error
				data, err = e.officeImageSource(ctx, sessionID, img.SourceID, img.SHA256)
				if err != nil {
					return err
				}
				totalBytes += len(data)
				if totalBytes > content.MaxInputBytes {
					return fmt.Errorf("%w: 图片资源合计超过 32 MiB，请拆分文件", domain.ErrInvalid)
				}
				cache[key] = data
			}
			img.Data = data
		}
	}
	return nil
}
