package attachmentapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/attachment"
)

// ReadOfficeSnapshot reuses the attachment's ownership checks and controlled
// file store. It never exposes its internal file reference to the renderer.
func (s *Service) ReadOfficeSnapshot(ctx context.Context, id, sessionID string) (attachment.Attachment, []byte, error) {
	a, err := s.GetAttachment(ctx, id)
	if err != nil {
		return attachment.Attachment{}, nil, err
	}
	if a == nil || a.SessionID != sessionID || s.fileStorage == nil || a.FileRef == "" || a.Size < 1 || a.Size > MaxFileSize {
		return attachment.Attachment{}, nil, errors.New("附件不属于当前会话或无法读取")
	}
	b, err := s.fileStorage.ReadFile(ctx, a.FileRef)
	if err != nil {
		return *a, nil, err
	}
	h := sha256.Sum256(b)
	if len(b) != int(a.Size) || hex.EncodeToString(h[:]) != a.SHA256 {
		return *a, nil, errors.New("附件摘要校验失败")
	}
	return *a, b, nil
}
