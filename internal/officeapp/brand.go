package officeapp

import (
	"encoding/json"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

type taskBrandRecord struct {
	Profile content.BrandProfile `json:"profile"`
	Asset   content.AssetRecord  `json:"asset"`
}

func BrandFromCheckpoint(raw json.RawMessage) (content.BrandProfile, content.AssetRecord, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return content.BrandProfile{}, content.AssetRecord{}, false
	}
	var rec taskBrandRecord
	if json.Unmarshal(fields["brand"], &rec) != nil || strings.TrimSpace(rec.Profile.BrandID) == "" {
		return content.BrandProfile{}, content.AssetRecord{}, false
	}
	return rec.Profile, rec.Asset, true
}

func WithTaskBrand(previous json.RawMessage, brand content.BrandProfile, asset content.AssetRecord) json.RawMessage {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(previous, &fields)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	fields["brand"] = encode(taskBrandRecord{Profile: brand, Asset: asset})
	return encode(fields)
}

func applyTaskBrand(task domain.Task, spec content.Spec) (content.Spec, error) {
	brand, asset, ok := BrandFromCheckpoint(task.Checkpoint)
	if !ok {
		return spec, nil
	}
	imported, err := content.ImportBrandL1(content.BrandImport{
		BrandID: brand.BrandID, Colors: brand.Colors, Fonts: brand.Fonts,
	}, asset)
	if err != nil {
		return spec, err
	}
	if strings.TrimSpace(spec.BrandID) == "" {
		spec.BrandID = imported.BrandID
	}
	return spec, nil
}
