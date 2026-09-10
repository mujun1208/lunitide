package toolruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/doctext"
)

const workspaceReadPageBytes = 3200
const workspaceReadPageChars = 3000

type workspaceReadArgs struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
}

func (r *Runtime) readWorkspace(ctx context.Context, mode Mode, session string, args workspaceReadArgs, unconfined bool) (Result, error) {
	offset, limit := 0, workspaceReadPageChars
	if args.Offset != nil {
		offset = *args.Offset
	}
	if args.Limit != nil {
		limit = *args.Limit
	}
	if offset < 0 || limit < 1 || limit > workspaceReadPageChars {
		return Result{}, errors.New("offset 不能为负，limit 必须是 1-3000 个字符")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	path, err := r.path(mode, session, args.Path, false, unconfined)
	if err != nil {
		return Result{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Result{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxGeneratedBytes {
		return Result{}, errors.New("文件不存在或超过 8 MiB 文档上限")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxGeneratedBytes+1))
	if err != nil {
		return Result{}, err
	}
	if len(raw) > maxGeneratedBytes {
		return Result{}, errors.New("文件超过 8 MiB 文档上限")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	text := string(raw)
	kind := "text"
	ext := strings.ToLower(filepath.Ext(path))
	isDocument := ext == ".docx" || ext == ".pptx" || ext == ".xlsx" || ext == ".pdf" || bytes.HasPrefix(raw, []byte("PK")) || bytes.HasPrefix(raw, []byte("%PDF-"))
	isImage := doctext.LooksLikeRasterImage(path, "", raw)
	if isDocument || isImage {
		// Reuse the application's isolated, time/memory bounded document parser.
		// The existing path resolver still decides what this tool may access.
		if r.documentText != nil {
			got, gotKind, method, _, readErr := r.documentText(ctx, path, raw, "")
			if readErr != nil {
				return Result{}, fmt.Errorf("文档无法读取（识别未完成）：%w", readErr)
			}
			text, kind = got, gotKind
			if strings.Contains(method, "incomplete-coverage") {
				kind = kind + "+incomplete"
			} else if method != "" && method != "text-layer" {
				kind = kind + "+ocr"
			}
		} else if isImage {
			return Result{}, errors.New("图片无法读取（OCR 未装配）")
		} else {
			extracted, err := doctext.ExtractContext(ctx, path, raw, "")
			if err != nil {
				if errors.Is(err, doctext.ErrNoTextLayer) {
					return Result{}, errors.New("文档无法读取（OCR 未装配）")
				}
				return Result{}, fmt.Errorf("文档无法读取：%w", err)
			}
			text, kind = extracted.Text, extracted.Kind
		}
	} else {
		if len(raw) > maxFile {
			return Result{}, errors.New("文本文件超过 1 MiB 上限")
		}
		if !utf8.Valid(raw) || bytes.ContainsRune(raw, 0) {
			return Result{}, errors.New("文件是二进制或不支持的文本编码，不能当纯文本编辑")
		}
	}
	if !isDocument && args.Offset == nil && args.Limit == nil && len(text) <= workspaceReadPageBytes {
		return result(text), nil // preserve existing small plain-text reads exactly
	}
	chars := []rune(text)
	if offset > len(chars) {
		return Result{}, errors.New("偏移已超出文件文本；若文件已变化，请从 offset=0 重新读取")
	}
	end, count := offset, 0
	for end < len(chars) && end-offset < limit {
		n := utf8.RuneLen(chars[end])
		if count+n > workspaceReadPageBytes {
			break
		}
		count += n
		end++
	}
	// Keep the cursor before the body, inside the chat tool summary budget.
	// Follow it until complete=true; no tail is silently passed off as read.
	header := fmt.Sprintf("[file-read kind=%s offset=%d nextOffset=%d totalChars=%d complete=%t]\n", kind, offset, end, len(chars), end == len(chars))
	return result(header + string(chars[offset:end])), nil
}
