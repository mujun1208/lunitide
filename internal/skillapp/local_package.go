package skillapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

var packageStorageMu sync.Mutex

// StoreLocalPackage stores inert, content-addressed resources. Importing never
// executes a script or enables a draft. The digest is committed in the skill
// manifest only after this function succeeds.
func StoreLocalPackage(rootDir string, files map[string][]byte) (string, error) {
	if rootDir == "" {
		return "", errors.New("skill package storage unavailable")
	}
	if err := validatePackageResources(files); err != nil {
		return "", err
	}
	raw, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	id := hex.EncodeToString(digest[:])
	packageStorageMu.Lock()
	defer packageStorageMu.Unlock()
	if err = os.MkdirAll(rootDir, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name := id + ".json"
	if existing, readErr := root.ReadFile(name); readErr == nil {
		if sha256.Sum256(existing) != digest {
			return "", errors.New("local skill package digest mismatch")
		}
		return id, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}
	f, err := os.CreateTemp(rootDir, ".package-*")
	if err != nil {
		return "", err
	}
	tempName := filepath.Base(f.Name())
	defer func() { _ = root.Remove(tempName) }()
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err = root.Link(tempName, name); err != nil {
		return "", err
	}
	return id, nil
}

func LoadLocalPackage(rootDir, digest string) (map[string][]byte, error) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 || rootDir == "" {
		return nil, errors.New("invalid local skill package identity")
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(digest + ".json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 96<<20+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 96<<20 {
		return nil, errors.New("local skill package exceeds storage limit")
	}
	actual := sha256.Sum256(raw)
	if hex.EncodeToString(actual[:]) != digest {
		return nil, errors.New("local skill package bytes changed")
	}
	var files map[string][]byte
	if err = json.Unmarshal(raw, &files); err != nil {
		return nil, err
	}
	if err = validatePackageResources(files); err != nil {
		return nil, err
	}
	return files, nil
}

func validatePackageResources(files map[string][]byte) error {
	if len(files) == 0 || len(files) > 4096 {
		return errors.New("skill package must have 1 to 4096 files")
	}
	total := 0
	seen := map[string]bool{}
	for name, raw := range files {
		if !PackageFilePath(name) || seen[strings.ToLower(name)] {
			return errors.New("invalid skill package resource path")
		}
		seen[strings.ToLower(name)] = true
		total += len(raw)
		if total > packageMaxBytes {
			return errors.New("skill package exceeds 64 MiB")
		}
	}
	for name := range files {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if seen[strings.ToLower(dir)] {
				return errors.New("skill resource file/directory conflict")
			}
		}
	}
	return nil
}

func (s *Service) SetPackageRoot(dir string) {
	s.packageMu.Lock()
	s.packageRoot = dir
	s.packageMu.Unlock()
}
func (s *Service) PackageRoot() string {
	s.packageMu.Lock()
	defer s.packageMu.Unlock()
	return s.packageRoot
}
func (s *Service) StorePackageFiles(files map[string][]byte) (string, error) {
	return StoreLocalPackage(s.PackageRoot(), files)
}
func (s *Service) PackageFiles(sk skill.Skill) (map[string][]byte, error) {
	files, err := PackageFiles(sk)
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Digest string `json:"localPackageDigest"`
	}
	if err = json.Unmarshal([]byte(sk.ManifestJSON), &manifest); err != nil {
		return nil, err
	}
	if manifest.Digest == "" {
		return files, nil
	}
	extra, err := LoadLocalPackage(s.PackageRoot(), manifest.Digest)
	if err != nil {
		return nil, err
	}
	destinationNames := make(map[string]string, len(files)+len(extra))
	for name := range files {
		destinationNames[strings.ToLower(name)] = name
	}
	putResource := func(name string, raw []byte) error {
		key := strings.ToLower(name)
		if present, exists := destinationNames[key]; exists && (present != name || string(files[present]) != string(raw)) {
			return errors.New("local skill resource collides with stored manifest")
		}
		files[name] = raw
		destinationNames[key] = name
		return nil
	}
	for name, raw := range extra {
		if name == "SKILL.md" {
			if string(files[name]) != string(raw) {
				if source, exists := extra["upstream/SKILL.md"]; exists && string(source) != string(raw) {
					return nil, errors.New("imported SKILL.md source path collides")
				}
				if err = putResource("upstream/SKILL.md", raw); err != nil {
					return nil, err
				}
			}
			continue
		}
		if name == "manifest.json" {
			if source, exists := extra["upstream/manifest.json"]; exists && string(source) != string(raw) {
				return nil, errors.New("imported manifest source path collides")
			}
			if err = putResource("upstream/manifest.json", raw); err != nil {
				return nil, err
			}
			continue
		}
		if err = putResource(name, raw); err != nil {
			return nil, err
		}
	}
	if err = validatePackageResources(files); err != nil {
		return nil, err
	}
	return files, nil
}
