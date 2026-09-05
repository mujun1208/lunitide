package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
)

func (a *OpenAI) Embed(ctx context.Context, secret []byte, model string, texts []string) ([][]float32, error) {
	if a == nil || a.c == nil {
		return nil, safeError("UPSTREAM_UNAVAILABLE", StageConnect, 0, "embed connector unavailable")
	}
	model = strings.TrimSpace(model)
	if model == "" || len(texts) == 0 {
		return nil, safeError("INVALID_REQUEST", StageDecode, 0, "embed model and texts required")
	}
	if len(texts) > 64 {
		texts = texts[:64]
	}
	body, err := marshalBounded(map[string]any{"model": model, "input": texts}, a.o.MaxRequestBytes)
	if err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return nil, classify(err)
	}
	req, err := a.c.NewRequest(ctx, http.MethodPost, "embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, classify(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := doWithSecret(a.c, req, "Authorization", "Bearer ", secret)
	if err != nil {
		return nil, uncertain(err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if readErr != nil {
		return nil, uncertain(readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, statusErrorReason(resp.StatusCode, boundedReason(bytes.NewReader(raw)))
	}
	out, ok := parseEmbeddingResponse(raw, len(texts))
	if !ok {
		return nil, safeError("INVALID_RESPONSE", StageDecode, resp.StatusCode, "embedding response missing vectors")
	}
	return out, nil
}

func parseEmbeddingResponse(raw []byte, want int) ([][]float32, bool) {
	var wrap struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &wrap) != nil || len(wrap.Data) == 0 {
		return nil, false
	}
	out := make([][]float32, want)
	seen := 0
	for _, row := range wrap.Data {
		i := row.Index
		if i < 0 || i >= want || len(row.Embedding) == 0 || out[i] != nil {
			if i == 0 && want == 1 && out[0] == nil && len(row.Embedding) > 0 {
				out[0] = row.Embedding
				seen = 1
				continue
			}
			if i >= want {
				continue
			}
		}
		if i >= 0 && i < want && out[i] == nil && len(row.Embedding) > 0 {
			out[i] = row.Embedding
			seen++
		}
	}
	if seen == 0 && want == len(wrap.Data) {
		for i, row := range wrap.Data {
			if len(row.Embedding) == 0 {
				return nil, false
			}
			out[i] = row.Embedding
			seen++
		}
	}
	if seen != want {
		return nil, false
	}
	return out, true
}

// EncodeEmbeddingBLOB writes uint32le dim | float32le[dim].
func EncodeEmbeddingBLOB(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	buf := make([]byte, 4+4*len(v))
	binary.LittleEndian.PutUint32(buf, uint32(len(v)))
	for i, x := range v {
		binary.LittleEndian.PutUint32(buf[4+4*i:], math.Float32bits(x))
	}
	return buf
}

// DecodeEmbeddingBLOB reads the P1 dense blob. Dim 0 or a size mismatch fails.
func DecodeEmbeddingBLOB(b []byte) ([]float32, bool) {
	if len(b) < 4 {
		return nil, false
	}
	dim := int(binary.LittleEndian.Uint32(b[:4]))
	if dim <= 0 || 4+4*dim != len(b) {
		return nil, false
	}
	out := make([]float32, dim)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4+4*i:]))
	}
	return out, true
}
