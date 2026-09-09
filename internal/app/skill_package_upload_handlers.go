package app

import (
	"context"
	"encoding/base64"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/skill"
)

type skillPackageUploader interface {
	SetPackageRoot(string)
	BeginPackageUpload(string, int, string) (string, error)
	AppendPackageUpload(string, int, []byte) (int, error)
	CommitPackageUpload(context.Context, string) (skill.Skill, error)
	AbortPackageUpload(string)
}

func handleSkillPackageUpload(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	svc, ok := e.skills.(skillPackageUploader)
	if !ok || e.persistDir == "" {
		return r.Fail("STORAGE_UNAVAILABLE", "技能包存储暂时不可用", true)
	}
	svc.SetPackageRoot(filepath.Join(e.persistDir, "skill-package-store"))
	switch string(r.Method) {
	case "skill.package.upload.begin":
		var p struct {
			Name   string `json:"name"`
			Size   int    `json:"size"`
			SHA256 string `json:"sha256"`
		}
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "技能上传参数无效", false)
		}
		id, err := svc.BeginPackageUpload(p.Name, p.Size, p.SHA256)
		if err != nil {
			return skillFailure(r, err)
		}
		return r.Ok(map[string]any{"uploadId": id, "chunkSize": 65536})
	case "skill.package.upload.chunk":
		var p struct {
			UploadID string `json:"uploadId"`
			Offset   int    `json:"offset"`
			Data     string `json:"dataBase64"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UploadID) || len(p.Data) > 87384 {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "技能上传分块无效", false)
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(p.Data)
		if err != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "技能分块编码无效", false)
		}
		received, err := svc.AppendPackageUpload(p.UploadID, p.Offset, raw)
		if err != nil {
			return skillFailure(r, err)
		}
		return r.Ok(map[string]any{"received": received})
	case "skill.package.upload.commit", "skill.package.upload.abort":
		var p struct {
			UploadID string `json:"uploadId"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UploadID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "技能上传标识无效", false)
		}
		if string(r.Method) == "skill.package.upload.abort" {
			svc.AbortPackageUpload(p.UploadID)
			return r.Ok(map[string]any{"aborted": true})
		}
		sk, err := svc.CommitPackageUpload(ctx, p.UploadID)
		if err != nil {
			return skillFailure(r, err)
		}
		return r.Ok(map[string]any{"skill": newSkillDTO(sk, e.skillCategoryFor(ctx, sk))})
	}
	return r.Fail("BRIDGE_SCHEMA_INVALID", "不支持的技能上传方法", false)
}
