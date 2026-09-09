package skillapp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/skillarchive"
	"github.com/oklog/ulid/v2"
)

type packageUpload struct {
	name, digest string
	size         int
	data         []byte
	expires      time.Time
	committed    *skill.Skill
}
type packageUploads struct {
	mu    sync.Mutex
	items map[string]*packageUpload
}

func (s *Service) uploads() *packageUploads {
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	if s.packageUploads == nil {
		s.packageUploads = &packageUploads{items: map[string]*packageUpload{}}
	}
	return s.packageUploads
}
func (u *packageUploads) cleanup(now time.Time) {
	for id, item := range u.items {
		if !now.Before(item.expires) {
			delete(u.items, id)
		}
	}
}

func (s *Service) BeginPackageUpload(name string, size int, digest string) (string, error) {
	if size < 2 || size > skillarchive.MaxArchiveBytes || len(name) > 240 || path.Base(name) != name || strings.ContainsAny(name, "\\:\x00") {
		return "", errors.New("技能包名称无效或超过 8 MiB")
	}
	ext := strings.ToLower(path.Ext(name))
	if ext != ".skill" && ext != ".zip" && ext != ".json" {
		return "", errors.New("请选择 .skill、.zip 或 JSON 技能包")
	}
	sha, err := hex.DecodeString(digest)
	if err != nil || len(sha) != 32 || strings.ToLower(digest) != digest {
		return "", errors.New("技能包摘要无效")
	}
	u := s.uploads()
	u.mu.Lock()
	defer u.mu.Unlock()
	now := s.clock.Now()
	u.cleanup(now)
	pending := 0
	for _, item := range u.items {
		if item.committed == nil {
			pending++
		}
	}
	if pending >= 4 || len(u.items) >= 128 {
		return "", errors.New("技能包上传任务过多，请先完成或取消上传")
	}
	id := ulid.Make().String()
	u.items[id] = &packageUpload{name: name, size: size, digest: digest, expires: now.Add(15 * time.Minute)}
	return id, nil
}

func (s *Service) AppendPackageUpload(id string, offset int, chunk []byte) (int, error) {
	u := s.uploads()
	u.mu.Lock()
	defer u.mu.Unlock()
	now := s.clock.Now()
	u.cleanup(now)
	item := u.items[id]
	if item == nil {
		return 0, errors.New("技能包上传已过期，请重新选择文件")
	}
	if item.committed != nil {
		return item.size, nil
	}
	if offset < 0 || len(chunk) == 0 || len(chunk) > 65536 || offset > len(item.data) || offset+len(chunk) > item.size {
		return 0, errors.New("技能包分块位置或大小无效")
	}
	if offset < len(item.data) {
		if offset+len(chunk) > len(item.data) || !bytes.Equal(chunk, item.data[offset:offset+len(chunk)]) {
			return 0, errors.New("技能包重传内容不一致")
		}
		return len(item.data), nil
	}
	item.data = append(item.data, chunk...)
	item.expires = now.Add(15 * time.Minute)
	return len(item.data), nil
}

func (s *Service) AbortPackageUpload(id string) {
	u := s.uploads()
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.items, id)
}

func (s *Service) CommitPackageUpload(ctx context.Context, id string) (skill.Skill, error) {
	u := s.uploads()
	u.mu.Lock()
	defer u.mu.Unlock()
	now := s.clock.Now()
	u.cleanup(now)
	item := u.items[id]
	if item == nil {
		return skill.Skill{}, errors.New("技能包上传已过期，请重新选择文件")
	}
	if item.committed != nil {
		return *item.committed, nil
	}
	if len(item.data) != item.size {
		return skill.Skill{}, errors.New("技能包尚未上传完整")
	}
	digest := sha256.Sum256(item.data)
	if hex.EncodeToString(digest[:]) != item.digest {
		return skill.Skill{}, errors.New("技能包校验失败；未导入")
	}
	sk, files, err := decodeUploadedSkill(item.name, item.data)
	if err != nil {
		return skill.Skill{}, err
	}
	if len(files) > 0 {
		stored, storeErr := s.StorePackageFiles(files)
		if storeErr != nil {
			return skill.Skill{}, storeErr
		}
		var manifest map[string]any
		if err = json.Unmarshal([]byte(sk.ManifestJSON), &manifest); err != nil {
			return skill.Skill{}, err
		}
		manifest["localPackageDigest"] = stored
		manifest["importScope"] = "complete_package"
		manifest["uploadedPackageHash"] = item.digest
		raw, marshalErr := json.Marshal(manifest)
		if marshalErr != nil {
			return skill.Skill{}, marshalErr
		}
		sk.ManifestJSON = string(raw)
	}
	created, err := s.Create(ctx, sk)
	if err != nil {
		return skill.Skill{}, err
	}
	item.committed = &created
	item.data = nil
	item.expires = now.Add(15 * time.Minute)
	return created, nil
}

func decodeUploadedSkill(filename string, data []byte) (skill.Skill, map[string][]byte, error) {
	if len(data) < 2 || len(data) > skillarchive.MaxArchiveBytes {
		return skill.Skill{}, nil, errors.New("技能包为空或超过 8 MiB")
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		var raw map[string]json.RawMessage
		if json.Unmarshal(data, &raw) != nil || raw == nil {
			return skill.Skill{}, nil, errors.New("技能包须为有效 ZIP 压缩包或 JSON")
		}
		var sk skill.Skill
		if json.Unmarshal(data, &sk) != nil {
			return sk, nil, errors.New("技能 JSON 格式无效")
		}
		// Local file metadata cannot assert a verified publisher or signature.
		sk.Signature = nil
		sk.PublisherID = nil
		if sk.Name == "" {
			sk.Name = strings.TrimSuffix(filename, path.Ext(filename))
		}
		if sk.DisplayName == "" {
			sk.DisplayName = sk.Name
		}
		if sk.Version == "" {
			sk.Version = "1.0.0"
		}
		if sk.EntryPoint == "" {
			sk.EntryPoint = "SKILL.md"
		}
		if len(sk.Permissions) == 0 {
			sk.Permissions = []skill.PermissionLevel{skill.PermissionReadOnly}
		}
		var manifest map[string]any
		if sk.ManifestJSON != "" {
			if json.Unmarshal([]byte(sk.ManifestJSON), &manifest) != nil {
				return sk, nil, errors.New("manifestJson 无效")
			}
		} else {
			if json.Unmarshal(data, &manifest) != nil {
				return sk, nil, errors.New("技能 JSON 无效")
			}
		}
		for _, field := range []string{"originSessionId", "localPackageDigest", "bundledPackage", "importCandidateId"} {
			delete(manifest, field)
		}
		encoded, err := json.Marshal(manifest)
		if err != nil {
			return sk, nil, err
		}
		sk.ManifestJSON = string(encoded)
		if prompt, ok := manifest["prompt"].(string); !ok || strings.TrimSpace(prompt) == "" {
			return sk, nil, errors.New("技能 JSON 缺少非空 prompt 正文")
		}
		if _, err = PackageFiles(sk); err != nil {
			return sk, nil, err
		}
		return sk, nil, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) > skillarchive.MaxFiles {
		return skill.Skill{}, nil, errors.New("技能 ZIP 无效或文件过多")
	}
	files := map[string][]byte{}
	seen := map[string]bool{}
	total := 0
	skillPath := ""
	for _, entry := range zr.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if !PackageFilePath(name) || (!entry.Mode().IsRegular() && !entry.Mode().IsDir()) || seen[strings.ToLower(name)] {
			return skill.Skill{}, nil, errors.New("技能 ZIP 含不安全路径、链接或重名文件")
		}
		seen[strings.ToLower(name)] = true
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.UncompressedSize64 > skillarchive.MaxExpandedBytes || uint64(total)+entry.UncompressedSize64 > skillarchive.MaxExpandedBytes {
			return skill.Skill{}, nil, errors.New("技能 ZIP 解压内容超过 32 MiB")
		}
		r, openErr := entry.Open()
		if openErr != nil {
			return skill.Skill{}, nil, openErr
		}
		raw, readErr := io.ReadAll(io.LimitReader(r, int64(skillarchive.MaxExpandedBytes-total)+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || len(raw) > skillarchive.MaxExpandedBytes-total {
			return skill.Skill{}, nil, errors.New("技能 ZIP 条目损坏或超过解压限制")
		}
		total += len(raw)
		files[name] = raw
		if path.Base(name) == "SKILL.md" {
			if skillPath != "" {
				return skill.Skill{}, nil, errors.New("技能包包含多个 SKILL.md；请一次上传一个技能目录")
			}
			skillPath = name
		}
	}
	if skillPath == "" {
		return skill.Skill{}, nil, errors.New("技能 ZIP 缺少 SKILL.md")
	}
	if len(files[skillPath]) > skillarchive.MaxPromptBytes {
		return skill.Skill{}, nil, errors.New("SKILL.md 超过 48 KiB，请将参考内容拆分到 references")
	}
	name, description, prompt, license, err := skillarchive.ParseDocument(files[skillPath])
	if err != nil {
		return skill.Skill{}, nil, err
	}
	base := path.Dir(skillPath)
	resources := map[string][]byte{}
	for name, raw := range files {
		rel := name
		if base != "." {
			if !strings.HasPrefix(name, base+"/") {
				return skill.Skill{}, nil, errors.New("技能包存在所选技能目录以外的文件；未静默丢弃")
			}
			rel = strings.TrimPrefix(name, base+"/")
		}
		resources[rel] = raw
	}
	manifest, err := json.Marshal(map[string]any{"prompt": prompt, "triggers": []string{name}, "license": license})
	if err != nil {
		return skill.Skill{}, nil, err
	}
	sk := skill.Skill{Name: name, DisplayName: name, Description: description, Version: "1.0.0", Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "SKILL.md", ManifestJSON: string(manifest)}
	if err = validatePackageResources(resources); err != nil {
		return skill.Skill{}, nil, err
	}
	return sk, resources, nil
}
