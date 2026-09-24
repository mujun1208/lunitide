package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/secretlease"
)

var completeOfficeTemplateName = func(e *Engine, ctx context.Context, fileName, excerpt string) (string, error) {
	return e.askOfficeTemplateName(ctx, fileName, excerpt)
}

func handleTemplateOfficeImport(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		FileName      string `json:"fileName"`
		UploadID      string `json:"uploadId"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.office.import 参数无效", false)
	}
	fileName := strings.TrimSpace(p.FileName)
	tplType := officeImportType(fileName)
	if tplType == "" || asset.ValidateTemplateFile(tplType, fileName) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "批量入库只接受 .pptx、.docx、.xlsx", false)
	}
	content, err := officeImportBytes(e, strings.TrimSpace(p.UploadID), p.ContentBase64)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.office.import 需要上传附件", false)
	}
	answer := ""
	if excerpt := officeImportExcerpt(fileName, content); excerpt != "" {
		answer, _ = completeOfficeTemplateName(e, ctx, fileName, excerpt)
	}
	name, desc := asset.OfficeTemplateLabel(fileName, answer)
	body := map[string]any{
		"name": name, "templateType": tplType, "description": desc, "fileName": fileName,
	}
	if id := strings.TrimSpace(p.UploadID); id != "" && strings.TrimSpace(p.ContentBase64) == "" {
		body["uploadId"] = id
	} else {
		body["contentBase64"] = p.ContentBase64
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return assetFailure(r, err)
	}
	next := r
	next.Payload = raw
	return handleTemplateCreate(e, ctx, next)
}

func officeImportType(fileName string) asset.TemplateType {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".pptx":
		return asset.TemplateTypePPT
	case ".docx":
		return asset.TemplateTypeWord
	case ".xlsx":
		return asset.TemplateTypeExcel
	default:
		return ""
	}
}

func officeImportBytes(e *Engine, uploadID, contentBase64 string) ([]byte, error) {
	switch {
	case uploadID != "" && strings.TrimSpace(contentBase64) != "":
		return nil, errors.New("both")
	case uploadID != "":
		return e.consumeTemplateStage(uploadID)
	case strings.TrimSpace(contentBase64) != "":
		if len(contentBase64) > base64.StdEncoding.EncodedLen(attachmentapp.MaxTemplateFileSize) {
			return nil, errors.New("large")
		}
		raw, err := base64.StdEncoding.DecodeString(contentBase64)
		if err != nil || len(raw) == 0 || len(raw) > attachmentapp.MaxTemplateFileSize {
			return nil, errors.New("invalid")
		}
		return raw, nil
	default:
		return nil, errors.New("missing")
	}
}

func officeImportExcerpt(fileName string, raw []byte) string {
	extracted, err := doctext.Extract(fileName, raw, "")
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(extracted.Text)
	if utf8.RuneCountInString(text) > 800 {
		text = string([]rune(text)[:800])
	}
	return text
}

func (e *Engine) askOfficeTemplateName(ctx context.Context, fileName, excerpt string) (string, error) {
	if e == nil || e.providers == nil {
		return "", errors.New("office template model unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, err := e.providers.List(ctx, provider.Filter{})
	if err != nil {
		return "", err
	}
	entry, ok := e.resolvePreferredChatModel(items)
	if !ok {
		return "", errors.New("office template model unavailable")
	}
	var output string
	err = e.withProviderLease(ctx, entry.Provider, secretlease.OperationChat, func(op context.Context, secret []byte) error {
		adapter, err := e.adapter(op, entry.Provider)
		if err != nil {
			return err
		}
		response, err := adapter.Complete(op, secret, llmadapter.Request{
			Model: entry.Model.ModelID, MaxTokens: 180, MaxAttempts: 1, DisableReasoning: true,
			Messages: []llmadapter.Message{
				{Role: "system", Content: "你在给一份办公模版起名。只输出两行，不要解释。\n名称：不超过20个字\n描述：一句话说明这份模版的用途"},
				{Role: "user", Content: "文件名：" + fileName + "\n正文：\n" + excerpt},
			},
		})
		if err != nil {
			return err
		}
		output = strings.TrimSpace(response.Message.Content)
		if output == "" {
			return errors.New("office template model returned no name")
		}
		return nil
	})
	return output, err
}
