package app

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/identity"
	"github.com/lunitide/lunitide/internal/people"
)

func handleIdentityGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "identity.get 参数无效", false)
	}
	if e.identity == nil {
		return peopleUnavailable(r)
	}
	return bridge.Success(r.ID, e.identity.Public())
}

func handleIdentityUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Nickname              *string          `json:"nickname"`
		Avatar                *string          `json:"avatar"`
		Status                *identity.Status `json:"status"`
		Department            *string          `json:"department"`
		Title                 *string          `json:"title"`
		OrgName               *string          `json:"orgName"`
		Bio                   *string          `json:"bio"`
		RegeneratePairingCode bool             `json:"regeneratePairingCode"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "identity.update 参数无效", false)
	}
	if e.identity == nil {
		return peopleUnavailable(r)
	}
	pub, err := e.identity.Update(ctx, identity.ProfilePatch{
		Nickname: p.Nickname, Avatar: p.Avatar, Status: p.Status,
		Department: p.Department, Title: p.Title, OrgName: p.OrgName, Bio: p.Bio,
	})
	if err != nil {
		return peopleFailure(r, err)
	}
	if p.RegeneratePairingCode {
		pub, err = e.identity.RotatePairingCode(ctx)
		if err != nil {
			return peopleFailure(r, err)
		}
	}
	if e.people != nil {
		e.people.RefreshPresence()
	}
	return bridge.Success(r.ID, pub)
}

func handleIdentityPasswordSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Password        string `json:"password"`
		CurrentPassword string `json:"currentPassword"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "identity.password.set 参数无效", false)
	}
	if e.identity == nil {
		return peopleUnavailable(r)
	}
	pub, err := e.identity.SetPassword(ctx, p.Password, p.CurrentPassword)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, pub)
}

func handleIdentityUnlock(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Password string `json:"password"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "identity.unlock 参数无效", false)
	}
	if e.identity == nil {
		return peopleUnavailable(r)
	}
	pub, err := e.identity.Unlock(p.Password)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, pub)
}

func handlePeopleList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.list 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	items, err := e.people.List(ctx)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"items": publicContacts(items)})
}

func handlePeoplePair(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PairingCode string `json:"pairingCode"`
		SubjectID   string `json:"subjectId"`
		Nickname    string `json:"nickname"`
		PublicKey   string `json:"publicKey"`
		Department  string `json:"department"`
		Title       string `json:"title"`
		OrgName     string `json:"orgName"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.pair 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	c, err := e.people.Pair(ctx, people.PairInput{
		PairingCode: p.PairingCode, SubjectID: p.SubjectID, Nickname: p.Nickname,
		PublicKey: p.PublicKey, Department: p.Department, Title: p.Title, OrgName: p.OrgName,
	})
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, publicContact(c))
}

func handlePeopleDiscoveryGet(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.discovery.get 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	enabled, code := e.people.DiscoveryGet()
	return bridge.Success(r.ID, map[string]any{"enabled": enabled, "pairingCode": code})
}

func handlePeopleDiscoverySet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Enabled bool `json:"enabled"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.discovery.set 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	pub, err := e.people.DiscoverySet(ctx, p.Enabled)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"enabled": pub.DiscoveryEnabled, "pairingCode": pub.PairingCode})
}

func handlePeopleThreadList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.thread.list 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	items, err := e.people.ListThreads(ctx)
	if err != nil {
		return peopleFailure(r, err)
	}
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, publicThread(t))
	}
	return bridge.Success(r.ID, map[string]any{"items": out})
}

func handlePeopleThreadOpen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ThreadID      string `json:"threadId"`
		PeerSubjectID string `json:"peerSubjectId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.thread.open 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	var (
		t    people.Thread
		msgs []people.Message
		err  error
	)
	switch {
	case p.ThreadID != "":
		t, msgs, err = e.people.OpenThread(ctx, p.ThreadID)
	case p.PeerSubjectID != "":
		t, msgs, err = e.people.OpenDirect(ctx, p.PeerSubjectID)
	default:
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.thread.open 需要 threadId 或 peerSubjectId", false)
	}
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"thread": publicThread(t), "messages": publicMessages(msgs)})
}

func handlePeopleThreadSend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ThreadID      string `json:"threadId"`
		Kind          string `json:"kind"`
		Body          string `json:"body"`
		FileName      string `json:"fileName"`
		FileMIME      string `json:"fileMime"`
		ContentBase64 string `json:"contentBase64"`
		LocalPath     string `json:"localPath"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.thread.send 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	msg, offer, err := e.people.Send(ctx, people.SendInput{
		ThreadID: p.ThreadID, Kind: p.Kind, Body: p.Body,
		FileName: p.FileName, FileMIME: p.FileMIME, ContentBase64: p.ContentBase64, LocalPath: p.LocalPath,
	})
	if err != nil {
		return peopleFailure(r, err)
	}
	out := map[string]any{"message": publicMessage(msg)}
	if offer != nil {
		out["offer"] = publicOffer(*offer)
	}
	return bridge.Success(r.ID, out)
}

func handlePeopleGroupCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Title            string   `json:"title"`
		OwnerSubjectID   string   `json:"ownerSubjectId"`
		MemberSubjectIDs []string `json:"memberSubjectIds"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.group.create 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	t, err := e.people.CreateGroup(ctx, p.Title, p.OwnerSubjectID, p.MemberSubjectIDs)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, publicThread(t))
}

func handlePeopleFileDecide(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		OfferID string `json:"offerId"`
		Accept  bool   `json:"accept"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.file.decide 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	offer, err := e.people.DecideFile(ctx, p.OfferID, p.Accept)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, publicOffer(offer))
}

func handlePeoplePeerAdd(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		HostAddr string `json:"hostAddr"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.peer.add 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	c, err := e.people.AddPeer(ctx, p.HostAddr)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, publicContact(c))
}

func handlePeopleContactUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SubjectID string  `json:"subjectId"`
		Remark    *string `json:"remark"`
		Blocked   *bool   `json:"blocked"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.contact.update 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	c, err := e.people.UpdateContact(ctx, p.SubjectID, people.ContactPatch{Remark: p.Remark, Blocked: p.Blocked})
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, publicContact(c))
}

func handlePeopleThreadTyping(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.thread.typing 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	if err := e.people.NoteTyping(ctx, p.ThreadID); err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, map[string]any{"ok": true})
}

func handlePeopleFileStage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		UploadID      string `json:"uploadId"`
		FileName      string `json:"fileName"`
		FileMIME      string `json:"fileMime"`
		Index         int    `json:"index"`
		Last          bool   `json:"last"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.file.stage 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	out, err := e.people.StageFile(ctx, people.StageInput{
		UploadID: p.UploadID, FileName: p.FileName, FileMIME: p.FileMIME,
		Index: p.Index, Last: p.Last, ContentBase64: p.ContentBase64,
	})
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, out)
}

func handlePeopleFilePick(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Folder bool `json:"folder"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.file.pick 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	out, err := e.people.PickFile(p.Folder)
	if err != nil {
		return peopleFailure(r, err)
	}
	return bridge.Success(r.ID, out)
}

func handlePeopleScreenCapture(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "people.screen.capture 参数无效", false)
	}
	if e.people == nil {
		return peopleUnavailable(r)
	}
	if e.ccctrl == nil {
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_CAPTURE_UNSUPPORTED", "当前环境无法直接截取本机画面", false)
	}
	png, err := e.ccctrl.CaptureDesktopPNG()
	if err != nil {
		if errors.Is(err, ccapp.ErrCcEngineUnavailable) {
			return bridge.Failure(r.ID, r.TraceID, "PEOPLE_CAPTURE_UNSUPPORTED", "当前环境无法直接截取本机画面", false)
		}
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_CAPTURE_FAILED", "无法截取本机画面", false)
	}
	if len(png) == 0 {
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_CAPTURE_FAILED", "无法截取本机画面", false)
	}
	return bridge.Success(r.ID, map[string]any{
		"contentBase64": base64.StdEncoding.EncodeToString(png),
		"mimeType":      "image/png",
	})
}

func peopleUnavailable(r bridge.Request) bridge.Response {
	return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "同事通讯录暂时不可用", true)
}

func peopleFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, identity.ErrLocked), errors.Is(err, people.ErrLocked):
		return bridge.Failure(r.ID, r.TraceID, "IDENTITY_LOCKED", "请先解锁本机个人资料", false)
	case errors.Is(err, identity.ErrPassword):
		return bridge.Failure(r.ID, r.TraceID, "IDENTITY_PASSWORD", "启动密码不正确", false)
	case errors.Is(err, identity.ErrAvatarUnreadable):
		return bridge.Failure(r.ID, r.TraceID, "IDENTITY_AVATAR", "图片无法读取", false)
	case errors.Is(err, identity.ErrInvalidProfile), errors.Is(err, identity.ErrPasswordTooLong), errors.Is(err, people.ErrInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "个人资料或同事请求无效", false)
	case errors.Is(err, people.ErrNotFound):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_NOT_FOUND", "同事或会话不存在", false)
	case errors.Is(err, people.ErrPairing):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_PAIRING", "配对码不正确", false)
	case errors.Is(err, people.ErrNotTrusted):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_NOT_TRUSTED", "未配对的局域网用户不能加入群聊", false)
	case errors.Is(err, people.ErrOfferDecided):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_OFFER_DECIDED", "该文件已确认过", false)
	case errors.Is(err, people.ErrTooLarge):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_TOO_LARGE", "文件超过 32 MiB 上限", false)
	case errors.Is(err, people.ErrBlocked):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_BLOCKED", "已屏蔽该同事", false)
	case errors.Is(err, people.ErrUnreachable):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_UNREACHABLE", "无法连接该地址，请确认对方已打开月汐", false)
	case errors.Is(err, people.ErrCanceled):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_CANCELED", "已取消选择", false)
	case errors.Is(err, people.ErrUnsupported):
		return bridge.Failure(r.ID, r.TraceID, "PEOPLE_PICKER_UNSUPPORTED", "当前系统没有可用的原生文件选择器，请改用拖入或发送文件", false)
	case errors.Is(err, identity.ErrUnavailable), errors.Is(err, people.ErrUnavailable):
		return peopleUnavailable(r)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "同事通讯录暂时不可用", true)
	}
}

func publicContact(c people.Contact) map[string]any {
	out := map[string]any{
		"subjectId": c.SubjectID, "nickname": c.Nickname, "avatar": c.Avatar, "status": c.Status,
		"department": c.Department, "title": c.Title, "orgName": c.OrgName, "bio": c.Bio,
		"publicKey": c.PublicKey, "trustState": c.TrustState, "hostAddr": c.HostAddr,
		"lastSeenAt": c.LastSeenAt, "createdAt": c.CreatedAt, "updatedAt": c.UpdatedAt, "self": c.Self,
		"blocked": c.Blocked,
	}
	if c.Remark != "" {
		out["remark"] = c.Remark
	}
	if c.LastReadAt != "" {
		out["lastReadAt"] = c.LastReadAt
	}
	return out
}

func publicContacts(items []people.Contact) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, c := range items {
		out = append(out, publicContact(c))
	}
	return out
}

func publicThread(t people.Thread) map[string]any {
	out := map[string]any{
		"threadId": t.ThreadID, "kind": t.Kind, "title": t.Title, "ownerSubjectId": t.OwnerID,
		"members": publicContacts(t.Members), "unreadCount": t.UnreadCount,
		"createdAt": t.CreatedAt, "updatedAt": t.UpdatedAt,
	}
	if t.LastMessage != nil {
		out["lastMessage"] = publicMessage(*t.LastMessage)
	}
	if len(t.TypingSubjectIDs) > 0 {
		out["typingSubjectIds"] = t.TypingSubjectIDs
	}
	return out
}

func publicMessage(m people.Message) map[string]any {
	out := map[string]any{
		"messageId": m.MessageID, "threadId": m.ThreadID, "senderSubjectId": m.SenderID,
		"kind": m.Kind, "body": m.Body, "createdAt": m.CreatedAt,
	}
	if m.FileName != "" {
		out["fileName"] = m.FileName
	}
	if m.FileMIME != "" {
		out["fileMime"] = m.FileMIME
	}
	if m.FileSize > 0 {
		out["fileSize"] = m.FileSize
	}
	if m.FileSHA256 != "" {
		out["fileSha256"] = m.FileSHA256
	}
	if m.OfferID != "" {
		out["offerId"] = m.OfferID
	}
	if m.OfferStatus != "" {
		out["offerStatus"] = m.OfferStatus
	}
	if m.DestPath != "" {
		out["destPath"] = m.DestPath
	}
	if m.TransferPercent > 0 {
		out["transferPercent"] = m.TransferPercent
	}
	return out
}

func publicMessages(items []people.Message) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, publicMessage(m))
	}
	return out
}

func publicOffer(o people.FileOffer) map[string]any {
	return map[string]any{
		"offerId": o.OfferID, "messageId": o.MessageID, "threadId": o.ThreadID,
		"fromSubjectId": o.FromID, "toSubjectId": o.ToID, "status": o.Status,
		"fileName": o.FileName, "fileMime": o.FileMIME, "fileSize": o.FileSize,
		"fileSha256": o.FileSHA256, "destPath": o.DestPath, "createdAt": o.CreatedAt, "decidedAt": o.DecidedAt,
	}
}
