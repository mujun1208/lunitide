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
		return Result{}, errors.New("offset must be nonnegative and limit must be 1-3000 characters")
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
		return Result{}, errors.New("file missing or exceeds 8 MiB document limit")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxGeneratedBytes+1))
	if err != nil {
		return Result{}, err
	}
	if len(raw) > maxGeneratedBytes {
		return Result{}, errors.New("file exceeds 8 MiB document limit")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	text := string(raw)
	kind := "text"
	ext := strings.ToLower(filepath.Ext(path))
	isDocument := ext == ".docx" || ext == ".pptx" || ext == ".xlsx" || ext == ".pdf" || bytes.HasPrefix(raw, []byte("PK")) || bytes.HasPrefix(raw, []byte("%PDF-"))
	if isDocument {
		// Reuse the application's isolated, time/memory bounded document parser.
		// The existing path resolver still decides what this tool may access.
		extracted, err := doctext.ExtractContext(ctx, path, raw, "")
		if err != nil {
			return Result{}, fmt.Errorf("document could not be read (scanned PDFs need OCR): %w", err)
		}
		text, kind = extracted.Text, extracted.Kind
	} else {
		if len(raw) > maxFile {
			return Result{}, errors.New("text file exceeds 1 MiB limit")
		}
		if !utf8.Valid(raw) || bytes.ContainsRune(raw, 0) {
			return Result{}, errors.New("file is binary or uses an unsupported text encoding; do not edit it as plain text")
		}
	}
	if !isDocument && args.Offset == nil && args.Limit == nil && len(text) <= workspaceReadPageBytes {
		return result(text), nil // preserve existing small plain-text reads exactly
	}
	chars := []rune(text)
	if offset > len(chars) {
		return Result{}, errors.New("offset is beyond the file text; restart from offset=0 if the file changed")
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
