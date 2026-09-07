package people

import (
	"context"
	"errors"
	"github.com/lunitide/lunitide/internal/imagepreview"
	"io"
	"os"
)

// PreviewFile reads only a local attachment visible in this identity's thread.
// It never accepts a path from the renderer or accepts an inbound offer.
func (s *Service) PreviewFile(ctx context.Context, offerID string) (string, error) {
	if err := s.readyUnlocked(); err != nil {
		return "", err
	}
	offer, err := s.store.GetOffer(ctx, offerID)
	if err != nil {
		return "", err
	}
	if _, err := s.PeekThread(ctx, offer.ThreadID); err != nil {
		return "", err
	}
	path := offer.DestPath
	if offer.FromID == s.identity.SubjectID() {
		path = offer.StagingPath
	} else if offer.Status != "accepted" {
		return "", ErrOfferDecided
	}
	if !pathUnderRoot(s.receiveDir, path) && !pathUnderRoot(s.stagingDir, path) {
		return "", ErrInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return "", ErrTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxFileBytes {
		return "", ErrTooLarge
	}
	preview, err := imagepreview.Encode(ctx, raw)
	if errors.Is(err, imagepreview.ErrTooLarge) {
		return "", ErrTooLarge
	}
	if errors.Is(err, imagepreview.ErrUnsupported) {
		return "", ErrUnsupported
	}
	return preview, err
}
