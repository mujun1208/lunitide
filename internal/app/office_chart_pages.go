package app

import (
	"context"
	"encoding/json"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

// Long category labels and six series names must not be truncated by the
// shared 4096-byte tool result boundary. Every page is valid complete JSON;
// chartJSON fragments concatenate to the exact serialized data contract.
func (e *Engine) officeChartPage(ctx context.Context, taskID, versionID, nodeID, nodeDigest string, offset int) (string, error) {
	v, data, err := e.officeStudio.ReadVersion(ctx, taskID, versionID)
	if err != nil {
		return "", err
	}
	c, err := content.ReadSlideChart(data, nodeID, nodeDigest)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	runes := []rune(string(encoded))
	if offset < 0 || offset > len(runes) {
		return "", domain.ErrInvalid
	}
	end := min(len(runes), offset+700)
	for {
		page := map[string]any{"taskId": taskID, "versionId": versionID, "nodeId": nodeID, "nodeDigest": nodeDigest, "sourceSha256": v.SHA256, "chartJSON": string(runes[offset:end]), "textOffset": offset, "nextTextOffset": end, "totalRunes": len(runes), "hasMore": end < len(runes), "notice": "Concatenate chartJSON in offset order, then parse the exact chart. Keep decimal strings and source frame geometry. Read every page before replacing."}
		result, err := officeToolJSON(page)
		if err == nil {
			return result, nil
		}
		if end <= offset {
			return "", err
		}
		end = offset + (end-offset)/2
	}
}
