package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillapp"
)

type skillCreationSessionKey struct{}

func withSkillCreationSession(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, skillCreationSessionKey{}, sessionID)
}
func skillCreationManifest(ctx context.Context, raw string) (string, error) {
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil || manifest == nil {
		return "", errors.New("skill manifest must be a JSON object")
	}
	// A model cannot forge this association. It is only a navigation aid, never
	// an authorization rule for the product's global local skill catalog.
	delete(manifest, "originSessionId")
	if sessionID, ok := ctx.Value(skillCreationSessionKey{}).(string); ok && validCanonicalULID(sessionID) {
		manifest["originSessionId"], _ = json.Marshal(sessionID)
	}
	data, err := json.Marshal(manifest)
	return string(data), err
}

type skillPackageEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int    `json:"size"`
}

func (e *Engine) loadSkillPackage(ctx context.Context, id string, checkedFile ...string) (*skill.Skill, map[string][]byte, string, string, error) {
	if !skillServiceAvailable(e.skills) {
		return nil, nil, "", "", errors.New("skill service unavailable")
	}
	sk, err := e.skills.Get(ctx, id)
	if err != nil {
		return nil, nil, "", "", err
	}
	if sk == nil {
		return nil, nil, "", "", skillapp.ErrSkillNotFound
	}
	files, err := e.skillPackageFiles(*sk)
	if err != nil {
		return nil, nil, "", "", err
	}
	if e.persistDir == "" {
		return nil, nil, "", "", errors.New("skill package storage unavailable")
	}
	var root, revision string
	if len(checkedFile) > 0 {
		root, revision, err = skillapp.CheckPackageFile(filepath.Join(e.persistDir, "skill-packages"), *sk, files, checkedFile[0])
	} else {
		root, revision, err = skillapp.MaterializePackage(filepath.Join(e.persistDir, "skill-packages"), *sk, files)
	}
	return sk, files, root, revision, err
}

func (e *Engine) skillPackageFiles(sk skill.Skill) (map[string][]byte, error) {
	if packages, ok := e.skills.(interface {
		PackageFiles(skill.Skill) (map[string][]byte, error)
	}); ok {
		return packages.PackageFiles(sk)
	}
	return skillapp.PackageFiles(sk)
}

func handleSkillPackageList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SkillID string `json:"skillId"`
		Cursor  string `json:"cursor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SkillID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "技能目录参数无效", false)
	}
	var checks []string
	if p.Cursor != "" {
		checks = []string{"manifest.json"}
	}
	_, files, root, revision, err := e.loadSkillPackage(ctx, p.SkillID, checks...)
	if err != nil {
		return skillFailure(r, err)
	}
	offset := 0
	if p.Cursor != "" {
		prev, index, found := strings.Cut(p.Cursor, ":")
		var parseErr error
		offset, parseErr = strconv.Atoi(index)
		if !found || prev != revision || parseErr != nil || offset < 0 {
			return r.Fail("VALIDATION_FAILED", "技能目录版本已变化，请刷新后重试", true)
		}
	}
	entries := map[string]skillPackageEntry{}
	for name, raw := range files {
		entries[name] = skillPackageEntry{Path: name, Kind: "file", Size: len(raw)}
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			entries[dir] = skillPackageEntry{Path: dir, Kind: "directory"}
		}
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	if offset > len(names) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "技能目录分页越界", false)
	}
	end := min(offset+128, len(names))
	page := make([]skillPackageEntry, 0, end-offset)
	for _, name := range names[offset:end] {
		page = append(page, entries[name])
	}
	result := map[string]any{"skillId": p.SkillID, "rootPath": root, "revision": revision, "entries": page}
	if end < len(names) {
		result["nextCursor"] = revision + ":" + strconv.Itoa(end)
	}
	return r.Ok(result)
}

func handleSkillPackageRead(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SkillID          string `json:"skillId"`
		Path             string `json:"path"`
		Offset           int    `json:"offset"`
		Limit            int    `json:"limit"`
		ExpectedRevision string `json:"expectedRevision"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SkillID) || !skillapp.PackageFilePath(p.Path) || p.Offset < 0 || p.Limit < 0 || p.Limit > 32768 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "技能文件参数无效", false)
	}
	_, files, _, revision, err := e.loadSkillPackage(ctx, p.SkillID, p.Path)
	if err != nil {
		return skillFailure(r, err)
	}
	if (p.Offset > 0 && p.ExpectedRevision == "") || (p.ExpectedRevision != "" && p.ExpectedRevision != revision) {
		return r.Fail("VALIDATION_FAILED", "技能文件版本已变化，请从头读取", true)
	}
	raw, ok := files[p.Path]
	if !ok {
		return r.Fail("NOT_FOUND", "技能文件不存在", false)
	}
	if p.Offset > len(raw) || (p.Offset < len(raw) && !utf8.RuneStart(raw[p.Offset])) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "技能文件分页越界或位于字符中间", false)
	}
	digest := sha256.Sum256(raw)
	result := map[string]any{"skillId": p.SkillID, "path": p.Path, "content": "", "encoding": "binary", "size": len(raw), "nextOffset": len(raw), "eof": true, "digest": hex.EncodeToString(digest[:]), "revision": revision}
	if utf8.Valid(raw) && !strings.ContainsRune(string(raw), '\x00') {
		limit := p.Limit
		if limit == 0 {
			limit = 16384
		}
		if limit < utf8.UTFMax {
			limit = utf8.UTFMax
		}
		end := min(p.Offset+limit, len(raw))
		for end < len(raw) && !utf8.RuneStart(raw[end]) {
			end--
		}
		result["content"] = string(raw[p.Offset:end])
		result["encoding"] = "utf8"
		result["nextOffset"] = end
		result["eof"] = end == len(raw)
	}
	return r.Ok(result)
}
