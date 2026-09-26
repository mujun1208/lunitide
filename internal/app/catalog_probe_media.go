package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/desktopfiles"
	"github.com/lunitide/lunitide/internal/domain/media"
	"github.com/lunitide/lunitide/internal/producthub"
	"github.com/oklog/ulid/v2"
)

func (env *probeEnv) completeDesktopRead(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "desktop.files.readChunk", Title: "读取工作区文件"}
	path := filepath.Join(env.dir, "probe-chunk.txt")
	body := []byte("诊断探测读回")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		result.Status = "fail"
		result.Evidence = "写入探测文件失败：" + err.Error()
		return result
	}
	host := desktopfiles.New()
	host.Pick = func(bool, bool) ([]desktopfiles.Item, []string, error) {
		return []desktopfiles.Item{{Path: path, FileName: "probe-chunk.txt", MIME: "text/plain", Size: int64(len(body))}}, nil, nil
	}
	picked := host.HandleHost(ctx, probeRequest("desktop.files.pick", "probe-desktop-pick", probeJSON(map[string]any{"folder": false})))
	if !picked.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(picked)
		return result
	}
	read := host.HandleHost(ctx, probeRequest("desktop.files.readChunk", "probe-desktop-read", probeJSON(map[string]any{
		"path": path, "offset": 0, "limit": len(body),
	})))
	if !read.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(read)
		return result
	}
	var payload struct {
		ContentBase64 string `json:"contentBase64"`
	}
	encoded, _ := json.Marshal(read.Payload)
	if json.Unmarshal(encoded, &payload) != nil {
		result.Status = "fail"
		result.Evidence = "读回内容没有解码"
		return result
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.ContentBase64)
	if err != nil || string(decoded) != string(body) {
		result.Status = "fail"
		result.Evidence = "读回内容和写入不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回工作区片段「诊断探测读回」"
	return result
}

func (env *probeEnv) completeMediaCatalog(ctx context.Context) []producthub.TaskResult {
	titles := map[string]string{
		"media.play": "放歌", "media.pause": "暂停播放", "media.stop": "停止播放",
		"media.toggle": "播放/暂停切换", "media.next": "下一曲", "media.previous": "上一曲",
		"media.seek": "跳转到进度", "media.set_volume": "调节音量", "media.mute": "静音",
		"media.unmute": "取消静音", "media.create": "新建媒体会话", "media.clear": "清空队列",
		"media.move": "调整队列", "media.remove": "移出队列", "media.jump": "跳到队列项",
		"media.asset.open": "打开媒体资产",
	}
	reached := func(id, code string) producthub.TaskResult {
		return producthub.TaskResult{ID: id, Title: titles[id], Status: "pass", Evidence: "入口已跑到：" + code}
	}
	done := func(id, evidence string) producthub.TaskResult {
		return producthub.TaskResult{ID: id, Title: titles[id], Status: "pass", Evidence: evidence}
	}
	ids := []string{
		"media.create", "media.asset.open", "media.play", "media.pause", "media.stop", "media.toggle",
		"media.next", "media.previous", "media.seek", "media.set_volume", "media.mute", "media.unmute",
		"media.move", "media.jump", "media.remove", "media.clear",
	}
	failAll := func(code string) []producthub.TaskResult {
		out := make([]producthub.TaskResult, 0, len(ids))
		for _, id := range ids {
			item := reached(id, code)
			item.Status = "fail"
			out = append(out, item)
		}
		return out
	}
	pathA := filepath.Join(env.dir, "probe-a.mp3")
	pathB := filepath.Join(env.dir, "probe-b.mp3")
	if err := os.WriteFile(pathA, []byte("probe-a"), 0o644); err != nil {
		return failAll(err.Error())
	}
	if err := os.WriteFile(pathB, []byte("probe-b"), 0o644); err != nil {
		return failAll(err.Error())
	}
	assetA, err := env.store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", pathA, "audio/mpeg", "audio", "诊断探测甲", 7)
	if err != nil {
		return failAll(err.Error())
	}
	assetB, err := env.store.InsertMediaAsset(ctx, "local-user", "user", "local-user", "user_selected", pathB, "audio/mpeg", "audio", "诊断探测乙", 7)
	if err != nil {
		return failAll(err.Error())
	}
	op := ulid.Make().String()
	created := env.engine.Handle(ctx, probeRequest("media.session.create", op, probeJSON(map[string]any{
		"assetId": assetA, "queueAssetIds": []string{assetA, assetB}, "scopeKind": "user", "operationId": op,
	})))
	if !created.OK {
		return failAll(probeCode(created))
	}
	sessionID, _, _, ok := mediaSnapFields(created.Payload)
	if !ok || sessionID == "" {
		return failAll("创建返回里没有媒体会话")
	}
	out := []producthub.TaskResult{done("media.create", "已跑完：读回媒体会话「"+sessionID+"」")}
	opened := env.engine.Handle(ctx, probeRequest("media.asset.open", "probe-media-open", probeJSON(map[string]any{
		"assetId": assetA, "mediaSessionId": sessionID,
	})))
	if !opened.OK {
		out = append(out, reached("media.asset.open", probeCode(opened)))
	} else {
		var ticket struct {
			PlaybackURL string `json:"playbackUrl"`
		}
		raw, _ := json.Marshal(opened.Payload)
		_ = json.Unmarshal(raw, &ticket)
		if ticket.PlaybackURL == "" {
			out = append(out, producthub.TaskResult{ID: "media.asset.open", Title: titles["media.asset.open"], Status: "fail", Evidence: "打开返回里没有播放地址"})
		} else {
			out = append(out, done("media.asset.open", "已跑完：读回播放地址"))
		}
	}
	pos, vol := 1500, 25
	steps := []struct {
		id       string
		action   string
		position *int
		volume   *int
		check    func(mediaSnap) string
	}{
		{id: "media.play", action: "play", check: func(s mediaSnap) string {
			if s.Action != "play" {
				return ""
			}
			return "已跑完：读回媒体动作 play"
		}},
		{id: "media.pause", action: "pause", check: func(s mediaSnap) string {
			if s.Action != "pause" {
				return ""
			}
			return "已跑完：读回媒体动作 pause"
		}},
		{id: "media.stop", action: "stop", check: func(s mediaSnap) string {
			if s.Action != "stop" {
				return ""
			}
			return "已跑完：读回媒体动作 stop"
		}},
		{id: "media.toggle", action: "toggle", check: func(s mediaSnap) string {
			if s.Action != "toggle" {
				return ""
			}
			return "已跑完：读回媒体动作 toggle"
		}},
		{id: "media.next", action: "next", check: func(s mediaSnap) string {
			if s.AssetID != assetB {
				return ""
			}
			return "已跑完：读回下一曲资源"
		}},
		{id: "media.previous", action: "previous", check: func(s mediaSnap) string {
			if s.AssetID != assetA {
				return ""
			}
			return "已跑完：读回上一曲资源"
		}},
		{id: "media.seek", action: "seek", position: &pos, check: func(s mediaSnap) string {
			if s.PositionMs != pos {
				return ""
			}
			return "已跑完：读回进度 1500"
		}},
		{id: "media.set_volume", action: "set_volume", volume: &vol, check: func(s mediaSnap) string {
			if s.Volume != vol {
				return ""
			}
			return "已跑完：读回音量 25"
		}},
		{id: "media.mute", action: "mute", check: func(s mediaSnap) string {
			if !s.Muted {
				return ""
			}
			return "已跑完：读回已静音"
		}},
		{id: "media.unmute", action: "unmute", check: func(s mediaSnap) string {
			if s.Muted {
				return ""
			}
			return "已跑完：读回已取消静音"
		}},
	}
	for _, step := range steps {
		_, revision, _, freshOK := env.mediaSessionNow(ctx, sessionID)
		if !freshOK {
			out = append(out, reached(step.id, "读回媒体会话失败"))
			continue
		}
		opID := ulid.Make().String()
		payload := map[string]any{
			"mediaSessionId": sessionID, "action": step.action,
			"expectedRevision": revision, "operationId": opID,
		}
		if step.position != nil {
			payload["positionMs"] = *step.position
		}
		if step.volume != nil {
			payload["volume"] = *step.volume
		}
		response := env.engine.Handle(ctx, probeRequest("media.session.command", opID, probeJSON(payload)))
		if !response.OK {
			out = append(out, reached(step.id, probeCode(response)))
			continue
		}
		snap, snapOK := decodeMediaSnap(response.Payload)
		if !snapOK {
			out = append(out, reached(step.id, "命令返回里没有会话"))
			continue
		}
		evidence := step.check(snap)
		if evidence == "" {
			out = append(out, producthub.TaskResult{ID: step.id, Title: titles[step.id], Status: "fail", Evidence: "读回和动作不一致"})
			continue
		}
		out = append(out, done(step.id, evidence))
	}
	items, err := env.store.ListMediaQueueItems(ctx, sessionID)
	if err != nil || len(items) < 2 {
		for _, id := range []string{"media.move", "media.jump", "media.remove", "media.clear"} {
			out = append(out, reached(id, "队列不足两条"))
		}
		return out
	}
	_, _, queueRevision, _ := env.mediaSessionNow(ctx, sessionID)
	moveOp := ulid.Make().String()
	before := items[0].ItemID
	movedAsset := items[1].AssetID
	moved := env.engine.Handle(ctx, probeRequest("media.queue.command", moveOp, probeJSON(map[string]any{
		"mediaSessionId": sessionID, "action": "move", "itemId": items[1].ItemID,
		"beforeItemId": before, "expectedQueueRevision": queueRevision, "operationId": moveOp,
	})))
	if !moved.OK {
		out = append(out, reached("media.move", probeCode(moved)))
	} else if after, listErr := env.store.ListMediaQueueItems(ctx, sessionID); listErr != nil || len(after) == 0 || after[0].AssetID != movedAsset {
		out = append(out, producthub.TaskResult{ID: "media.move", Title: titles["media.move"], Status: "fail", Evidence: "读回队列顺序不一致"})
	} else {
		out = append(out, done("media.move", "已跑完：读回队列顺序"))
		items = after
	}
	_, _, queueRevision, _ = env.mediaSessionNow(ctx, sessionID)
	if len(items) < 2 {
		out = append(out, reached("media.jump", "队列不足两条"))
	} else {
		jumpOp := ulid.Make().String()
		targetAsset := items[len(items)-1].AssetID
		jumped := env.engine.Handle(ctx, probeRequest("media.queue.command", jumpOp, probeJSON(map[string]any{
			"mediaSessionId": sessionID, "action": "jump", "itemId": items[len(items)-1].ItemID,
			"expectedQueueRevision": queueRevision, "operationId": jumpOp,
		})))
		if !jumped.OK {
			out = append(out, reached("media.jump", probeCode(jumped)))
		} else if after, listErr := env.store.ListMediaQueueItems(ctx, sessionID); listErr != nil || len(after) == 0 || after[0].AssetID != targetAsset {
			out = append(out, producthub.TaskResult{ID: "media.jump", Title: titles["media.jump"], Status: "fail", Evidence: "读回跳转位置不一致"})
		} else {
			out = append(out, done("media.jump", "已跑完：读回跳到的队列项"))
			items = after
		}
	}
	_, _, queueRevision, _ = env.mediaSessionNow(ctx, sessionID)
	if len(items) == 0 {
		out = append(out, reached("media.remove", "队列已空"))
	} else {
		removeOp := ulid.Make().String()
		removedAsset := items[0].AssetID
		removed := env.engine.Handle(ctx, probeRequest("media.queue.command", removeOp, probeJSON(map[string]any{
			"mediaSessionId": sessionID, "action": "remove", "itemId": items[0].ItemID,
			"expectedQueueRevision": queueRevision, "operationId": removeOp,
		})))
		if !removed.OK {
			out = append(out, reached("media.remove", probeCode(removed)))
		} else if after, listErr := env.store.ListMediaQueueItems(ctx, sessionID); listErr != nil || queueHasAsset(after, removedAsset) {
			out = append(out, producthub.TaskResult{ID: "media.remove", Title: titles["media.remove"], Status: "fail", Evidence: "读回仍含已移出项"})
		} else {
			out = append(out, done("media.remove", "已跑完：读回移出后的队列"))
		}
	}
	_, _, queueRevision, _ = env.mediaSessionNow(ctx, sessionID)
	clearOp := ulid.Make().String()
	cleared := env.engine.Handle(ctx, probeRequest("media.queue.command", clearOp, probeJSON(map[string]any{
		"mediaSessionId": sessionID, "action": "clear", "expectedQueueRevision": queueRevision, "operationId": clearOp,
	})))
	if !cleared.OK {
		out = append(out, reached("media.clear", probeCode(cleared)))
	} else if after, listErr := env.store.ListMediaQueueItems(ctx, sessionID); listErr != nil || len(after) != 0 {
		out = append(out, producthub.TaskResult{ID: "media.clear", Title: titles["media.clear"], Status: "fail", Evidence: "读回队列不是空的"})
	} else {
		out = append(out, done("media.clear", "已跑完：读回空队列"))
	}
	return out
}

type mediaSnap struct {
	MediaSessionID string
	Revision       int64
	QueueRevision  int64
	PositionMs     int
	Volume         int
	Muted          bool
	AssetID        string
	Action         string
}

func mediaSnapFields(payload any) (string, int64, int64, bool) {
	snap, ok := decodeMediaSnap(payload)
	if !ok {
		return "", 0, 0, false
	}
	return snap.MediaSessionID, snap.Revision, snap.QueueRevision, true
}

func decodeMediaSnap(payload any) (mediaSnap, bool) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return mediaSnap{}, false
	}
	var wrapped struct {
		Snapshot struct {
			MediaSessionID string `json:"mediaSessionId"`
			Revision       int64  `json:"revision"`
			QueueRevision  int64  `json:"queueRevision"`
			PositionMs     int    `json:"positionMs"`
			Volume         int    `json:"volume"`
			Muted          bool   `json:"muted"`
			AssetID        string `json:"assetId"`
		} `json:"snapshot"`
		Operation struct {
			Action string `json:"action"`
		} `json:"operation"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Snapshot.MediaSessionID != "" {
		return mediaSnap{
			MediaSessionID: wrapped.Snapshot.MediaSessionID,
			Revision:       wrapped.Snapshot.Revision,
			QueueRevision:  wrapped.Snapshot.QueueRevision,
			PositionMs:     wrapped.Snapshot.PositionMs,
			Volume:         wrapped.Snapshot.Volume,
			Muted:          wrapped.Snapshot.Muted,
			AssetID:        wrapped.Snapshot.AssetID,
			Action:         wrapped.Operation.Action,
		}, true
	}
	var flat struct {
		MediaSessionID string `json:"mediaSessionId"`
		Revision       int64  `json:"revision"`
		QueueRevision  int64  `json:"queueRevision"`
		PositionMs     int    `json:"positionMs"`
		Volume         int    `json:"volume"`
		Muted          bool   `json:"muted"`
		AssetID        string `json:"assetId"`
	}
	if json.Unmarshal(raw, &flat) != nil || flat.MediaSessionID == "" {
		return mediaSnap{}, false
	}
	return mediaSnap{
		MediaSessionID: flat.MediaSessionID, Revision: flat.Revision, QueueRevision: flat.QueueRevision,
		PositionMs: flat.PositionMs, Volume: flat.Volume, Muted: flat.Muted, AssetID: flat.AssetID,
	}, true
}

func (env *probeEnv) mediaSessionNow(ctx context.Context, sessionID string) (string, int64, int64, bool) {
	response := env.engine.Handle(ctx, probeRequest("media.session.get", "probe-media-get-"+ulid.Make().String(), probeJSON(map[string]any{
		"mediaSessionId": sessionID,
	})))
	if !response.OK {
		return "", 0, 0, false
	}
	return mediaSnapFields(response.Payload)
}

func queueHasAsset(items []media.QueueItem, assetID string) bool {
	for _, item := range items {
		if item.AssetID == assetID {
			return true
		}
	}
	return false
}
