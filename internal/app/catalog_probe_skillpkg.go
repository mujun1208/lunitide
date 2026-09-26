package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/producthub"
)

func (env *probeEnv) completeSkillPackage(ctx context.Context) producthub.TaskResult {
	result := producthub.TaskResult{ID: "skill.package.upload.commit", Title: "安装技能"}
	env.engine.SetPersistDir(env.dir)
	prompt := "---\nname: catalog-probe-pack\ndescription: 诊断探测技能包。\n---\n诊断探测。\n"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	file, err := zw.Create("catalog-probe-pack/SKILL.md")
	if err != nil {
		result.Status = "fail"
		result.Evidence = "打包探测技能失败：" + err.Error()
		return result
	}
	if _, err = file.Write([]byte(prompt)); err != nil {
		result.Status = "fail"
		result.Evidence = "写入探测技能失败：" + err.Error()
		return result
	}
	if err = zw.Close(); err != nil {
		result.Status = "fail"
		result.Evidence = "关闭探测技能包失败：" + err.Error()
		return result
	}
	zipped := buf.Bytes()
	sum := sha256.Sum256(zipped)
	begun := env.engine.Handle(ctx, probeRequest("skill.package.upload.begin", "probe-skill-pack-begin", probeJSON(map[string]any{
		"name": "catalog-probe-pack.skill", "size": len(zipped), "sha256": hex.EncodeToString(sum[:]),
	})))
	if !begun.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(begun)
		return result
	}
	var beginBody struct {
		UploadID string `json:"uploadId"`
	}
	raw, _ := json.Marshal(begun.Payload)
	if json.Unmarshal(raw, &beginBody) != nil || beginBody.UploadID == "" {
		result.Status = "fail"
		result.Evidence = "上传开始没有编号"
		return result
	}
	chunk := env.engine.Handle(ctx, probeRequest("skill.package.upload.chunk", "probe-skill-pack-chunk", probeJSON(map[string]any{
		"uploadId": beginBody.UploadID, "offset": 0, "dataBase64": base64.StdEncoding.EncodeToString(zipped),
	})))
	if !chunk.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(chunk)
		return result
	}
	committed := env.engine.Handle(ctx, probeRequest("skill.package.upload.commit", "probe-skill-pack-commit", probeJSON(map[string]any{
		"uploadId": beginBody.UploadID,
	})))
	if !committed.OK {
		result.Status = "pass"
		result.Evidence = "入口已跑到：" + probeCode(committed)
		return result
	}
	var body struct {
		Skill struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"skill"`
	}
	raw, _ = json.Marshal(committed.Payload)
	if json.Unmarshal(raw, &body) != nil || body.Skill.ID == "" {
		result.Status = "fail"
		result.Evidence = "安装返回里没有技能编号"
		return result
	}
	stored, err := env.store.GetSkill(ctx, body.Skill.ID)
	if err != nil || stored == nil || stored.Name != "catalog-probe-pack" {
		result.Status = "fail"
		result.Evidence = "读回技能包不一致"
		return result
	}
	result.Status = "pass"
	result.Evidence = "已跑完：读回技能包「catalog-probe-pack」"
	return result
}
