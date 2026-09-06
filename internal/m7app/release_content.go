package m7app

import (
	"encoding/base64"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

type ReleaseBlobReader interface{ GetReleaseBlob(string) (string, error) }

// ReadReleaseMember checks the immutable captured bytes, not a caller digest.
func ReadReleaseMember(tx ReleaseBlobReader, digest string, size int64) ([]byte, error) {
	if size < 1 || size > 10<<20 {
		return nil, fmt.Errorf("%w: invalid member size", ErrPackageInvalid)
	}
	blob, err := tx.GetReleaseBlob(digest)
	if err != nil {
		return nil, fmt.Errorf("%w: member blob missing", ErrEvidenceMissing)
	}
	if len(blob) > base64.StdEncoding.EncodedLen(10<<20) {
		return nil, ErrPackageInvalid
	}
	data, err := base64.StdEncoding.DecodeString(blob)
	if err != nil || int64(len(data)) != size || m7flow.SHA256Hex(data) != digest {
		return nil, fmt.Errorf("%w: member bytes", ErrDigestMismatch)
	}
	return data, nil
}

func VerifyReleaseContent(tx ReleaseBlobReader, doc m7flow.SealedPackageDoc) error {
	if doc.Manifest["contentVerified"] != true || doc.SBOM == nil || len(doc.Members) < 1 {
		return fmt.Errorf("%w: package has no captured project content", ErrEvidenceMissing)
	}
	var total int64
	for _, member := range doc.Members {
		total += member.Size
		if total > 30<<20 {
			return ErrPackageInvalid
		}
		if _, err := ReadReleaseMember(tx, member.SHA256, member.Size); err != nil {
			return err
		}
	}
	blob, err := tx.GetReleaseBlob(doc.SBOM.Digest)
	if err != nil {
		return ErrEvidenceMissing
	}
	if len(blob) > 1<<20 || m7flow.SHA256Hex([]byte(blob)) != doc.SBOM.Digest {
		return fmt.Errorf("%w: source inventory bytes", ErrDigestMismatch)
	}
	return nil
}
