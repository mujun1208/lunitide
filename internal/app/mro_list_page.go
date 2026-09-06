package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/mroapp"
)

const mroPageRows = 100
const mroPageBytes = 1 << 20

type mroPagePosition struct {
	Row   int `json:"r"`
	Child int `json:"c,omitempty"`
}
type mroPageCursor struct {
	Digest string                     `json:"d"`
	After  map[string]mroPagePosition `json:"a"`
}

func mroPageQuery(raw json.RawMessage) (map[string]json.RawMessage, string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, "", mroapp.ErrPayloadInvalid
	}
	var cursor string
	if value, ok := fields["cursor"]; ok {
		if string(value) == "null" || json.Unmarshal(value, &cursor) != nil || len(cursor) > 1024 {
			return nil, "", mroapp.ErrPayloadInvalid
		}
		delete(fields, "cursor")
	}
	return fields, cursor, nil
}

func decodeMROPagePayload(raw json.RawMessage, target any) error {
	fields, _, err := mroPageQuery(raw)
	if err != nil {
		return err
	}
	clean, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return decodePayload(clean, target)
}

// mroPageRow preserves complete scalar fields and splits only declared child
// collections. A large component history therefore remains reachable without
// raising the IPC budget or truncating notes. Child fragments retain the parent.
func mroPageRow(raw json.RawMessage, offset int) (json.RawMessage, int, error) {
	var row map[string]json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, 0, err
	}
	for _, key := range []string{"events", "tails", "missing", "sources"} {
		value, exists := row[key]
		if !exists {
			continue
		}
		var children []json.RawMessage
		if err := json.Unmarshal(value, &children); err != nil {
			return nil, 0, err
		}
		if offset < 0 || offset > len(children) || (offset > 0 && offset == len(children)) {
			return nil, 0, mroapp.ErrPayloadInvalid
		}
		end := min(offset+mroPageRows, len(children))
		part := append([]json.RawMessage{}, children[offset:end]...)
		var err error
		row[key], err = json.Marshal(part)
		if err != nil {
			return nil, 0, err
		}
		encoded, err := json.Marshal(row)
		if end == len(children) {
			end = 0
		}
		return encoded, end, err
	}
	if offset != 0 {
		return nil, 0, mroapp.ErrPayloadInvalid
	}
	return raw, 0, nil
}

// The service still evaluates the entire supported organization dataset in its
// original transaction. Only this public projection is paginated. The digest
// binds business content, filters, method and trusted organization; no volatile
// read/check timestamp is introduced into the cursor.
func mroPageResponse(ctx context.Context, r bridge.Request, payload map[string]any) bridge.Response {
	query, cursor, err := mroPageQuery(r.Payload)
	if err != nil {
		return mroFailure(r, err)
	}
	rows := make(map[string][]json.RawMessage, len(payload))
	keys := make([]string, 0, len(payload))
	digest := sha256.New()
	encoder := json.NewEncoder(digest)
	for _, value := range []any{r.Method, mroapp.Scope(ctx), query} {
		if err := encoder.Encode(value); err != nil {
			return mroFailure(r, err)
		}
	}
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := reflect.ValueOf(payload[key])
		if value.Kind() != reflect.Slice {
			return mroFailure(r, errors.New("MRO list projection must contain arrays"))
		}
		if err := encoder.Encode(key); err != nil {
			return mroFailure(r, err)
		}
		for i := 0; i < value.Len(); i++ {
			if err := ctx.Err(); err != nil {
				return mroFailure(r, err)
			}
			raw, err := json.Marshal(value.Index(i).Interface())
			if err != nil {
				return mroFailure(r, err)
			}
			rows[key] = append(rows[key], raw)
			if err := encoder.Encode(raw); err != nil {
				return mroFailure(r, err)
			}
		}
		if err := encoder.Encode(value.Len()); err != nil {
			return mroFailure(r, err)
		}
	}
	token := mroPageCursor{Digest: hex.EncodeToString(digest.Sum(nil)), After: map[string]mroPagePosition{}}
	if cursor != "" {
		var previous mroPageCursor
		raw, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil || decodePayload(raw, &previous) != nil || previous.After == nil || len(previous.After) != len(keys) {
			return r.Fail("MRO_PAGE_CHANGED", "列表游标无效，请重新读取第一页", true)
		}
		if previous.Digest != token.Digest {
			return r.Fail("MRO_PAGE_CHANGED", "列表或当前范围已变化，请刷新后继续", true)
		}
		token.After = previous.After
	}
	result := make(map[string]any, len(keys)+2)
	continued := []string{}
	bytesUsed := 8192 // Reserved for the cursor, collection names and envelope.
	pending := false
	for _, key := range keys {
		position, exists := token.After[key]
		if cursor != "" && (!exists || position.Row < 0 || position.Row > len(rows[key]) || position.Child < 0 || (position.Row == len(rows[key]) && position.Child != 0)) {
			return r.Fail("MRO_PAGE_CHANGED", "列表游标位置无效，请重新读取第一页", true)
		}
		page := []json.RawMessage{}
		for position.Row < len(rows[key]) && len(page) < mroPageRows {
			part, nextChild, err := mroPageRow(rows[key][position.Row], position.Child)
			if err != nil {
				return r.Fail("MRO_PAGE_CHANGED", "列表游标内容无效，请重新读取第一页", true)
			}
			if len(part)+8192 > mroPageBytes {
				return r.Fail("MRO_CAPACITY", "单条记录超过读取预算，请核查来源内容", false)
			}
			if bytesUsed+len(part)+1 > mroPageBytes {
				break
			}
			if position.Child != 0 && len(page) == 0 {
				continued = append(continued, key)
			}
			page = append(page, part)
			bytesUsed += len(part) + 1
			position.Child = nextChild
			if nextChild != 0 {
				break
			}
			position.Row++
		}
		result[key] = page
		token.After[key] = position
		pending = pending || position.Row < len(rows[key])
	}
	if len(continued) != 0 {
		result["continuedFields"] = continued
	}
	if pending {
		encoded, err := json.Marshal(token)
		if err != nil {
			return mroFailure(r, err)
		}
		result["nextCursor"] = base64.RawURLEncoding.EncodeToString(encoded)
	}
	response := r.Ok(result)
	raw, err := json.Marshal(response)
	if err != nil || len(raw) > mroPageBytes {
		return mroFailure(r, fmt.Errorf("MRO page budget: %w", mroapp.ErrCapacity))
	}
	return response
}

func mroDueWriteResponse(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	// A mutation is executed once; its remaining projection is read via due.list.
	// Recompute can change the SQL ordering key; read its committed projection
	// in the enclosing transaction so the next read uses the identical order.
	items, err := e.mro.ListDue(ctx)
	if err != nil {
		return mroFailure(r, err)
	}
	r.Method, r.Payload = "mro.due.list", json.RawMessage(`{}`)
	return mroPageResponse(ctx, r, map[string]any{"items": mroDuePayload(items)})
}
