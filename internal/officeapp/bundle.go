package officeapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/oklog/ulid/v2"
)

func (s *Service) CreateBundle(ctx context.Context, taskID, title string, versionIDs []string, key string) (domain.Bundle, error) {
	d, err := s.deliveries()
	if err != nil {
		return domain.Bundle{}, err
	}
	if len(versionIDs) == 0 || len(versionIDs) > 32 {
		return domain.Bundle{}, domain.ErrInvalid
	}
	b := domain.Bundle{TaskID: taskID, Title: title, Files: []domain.BundleFile{}}
	var bytes int64
	seen := map[string]bool{}
	for index, id := range versionIDs {
		v, _, e := s.ReadVersion(ctx, taskID, id)
		if e != nil {
			return b, e
		}
		if seen[v.ArtifactID] {
			return b, domain.ErrInvalid
		}
		seen[v.ArtifactID] = true
		bytes += v.Size
		if bytes > 256<<20 {
			return b, errors.New("成套文件超过 256 MiB，请拆分交付包")
		}
		base := []rune(strings.TrimSuffix(v.Name, filepath.Ext(v.Name)))
		if len(base) > 150 {
			base = base[:150]
		}
		name := fmt.Sprintf("%02d-%s-v%d-%s.%s", index+1, string(base), v.VersionNo, v.ID, v.Kind)
		b.Files = append(b.Files, domain.BundleFile{VersionID: v.ID, ArtifactID: v.ArtifactID, Name: name, Kind: v.Kind, SHA256: v.SHA256, Size: v.Size})
	}
	if err := s.assertBundleFactSet(ctx, taskID, versionIDs); err != nil {
		return domain.Bundle{}, err
	}
	return d.CreateOfficeBundle(ctx, b, key)
}

func (s *Service) assertBundleFactSet(ctx context.Context, taskID string, versionIDs []string) error {
	task, err := s.Store.GetOfficeTask(ctx, taskID)
	if err != nil {
		return err
	}
	facts := append([]content.Fact(nil), BriefFromCheckpoint(task.Checkpoint).Facts...)
	for _, id := range versionIDs {
		v, _, err := s.ReadVersion(ctx, taskID, id)
		if err != nil {
			return err
		}
		var spec content.Spec
		if json.Unmarshal(v.Spec, &spec) != nil {
			continue
		}
		facts = append(facts, content.FactsFromSpec(spec)...)
	}
	locked := false
	for _, f := range facts {
		if f.Locked {
			locked = true
			break
		}
	}
	if !locked {
		return nil
	}
	return s.AssertTaskFactSet(ctx, taskID, facts, versionIDs)
}

func (s *Service) ListBundles(ctx context.Context, taskID string) ([]domain.Bundle, error) {
	d, err := s.deliveries()
	if err != nil {
		return nil, err
	}
	return d.ListOfficeBundles(ctx, taskID)
}

type BundleExportFile struct {
	VersionID string `json:"versionId"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Reused    bool   `json:"reused"`
}

type BundleExport struct {
	BundleID     string             `json:"bundleId"`
	Directory    string             `json:"directory"`
	ManifestPath string             `json:"manifestPath"`
	Files        []BundleExportFile `json:"files"`
	Complete     bool               `json:"complete"`
	Notice       string             `json:"notice,omitempty"`
}

// ExportBundle uses the saved manifest, never whichever versions happen to be
// current during export. Partial exports remain retryable; changed external
// files are never overwritten and every completed file gets a durable receipt.
func (s *Service) ExportBundle(ctx context.Context, taskID, bundleID, dir string) (BundleExport, error) {
	out := BundleExport{BundleID: bundleID, Files: []BundleExportFile{}}
	d, err := s.deliveries()
	if err != nil {
		return out, err
	}
	b, err := d.GetOfficeBundle(ctx, bundleID)
	if err != nil {
		return out, err
	}
	if b.TaskID != taskID {
		return out, domain.ErrScope
	}
	if !filepath.IsAbs(dir) {
		return out, domain.ErrInvalid
	}
	base, err := os.OpenRoot(dir)
	if err != nil {
		return out, err
	}
	defer base.Close()
	folder := "bundle-" + b.ID
	if err = base.Mkdir(folder, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return out, err
	}
	r, err := base.OpenRoot(folder)
	if err != nil {
		return out, err
	}
	defer r.Close()
	out.Directory = filepath.Join(dir, folder)
	out.ManifestPath = filepath.Join(out.Directory, "manifest.json")
	manifest, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return out, err
	}
	manifest = append(manifest, '\n')
	if _, err = writeBundleFile(ctx, r, "manifest.json", manifest); err != nil {
		return out, err
	}
	for _, f := range b.Files {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		v, data, e := s.ReadVersion(ctx, taskID, f.VersionID)
		if e != nil {
			return out, e
		}
		if v.SHA256 != f.SHA256 || v.Size != f.Size || v.ArtifactID != f.ArtifactID {
			return out, domain.ErrConflict
		}
		reused, e := writeBundleFile(ctx, r, f.Name, data)
		if e != nil {
			return out, e
		}
		path := filepath.Join(out.Directory, f.Name)
		task, e := s.Store.GetOfficeTask(ctx, taskID)
		if e != nil {
			return out, e
		}
		runID := task.RunID
		if runID == "" {
			runID = task.ID
		}
		e = s.Store.AppendOfficeStepReceipt(ctx, domain.StepReceipt{TaskID: taskID, RunID: runID, StepKey: "bundle.export", IdempotencyKey: "bundle-" + digest([]byte(bundleID+"\n"+path)), InputDigest: f.SHA256, State: "succeeded", Result: encode(map[string]string{"bundleId": bundleID, "versionId": v.ID, "path": path, "sha256": f.SHA256})})
		if e != nil {
			return out, fmt.Errorf("文件已导出，但回执未保存，请重试核对：%w", e)
		}
		out.Files = append(out.Files, BundleExportFile{VersionID: v.ID, Name: f.Name, Path: path, SHA256: f.SHA256, Reused: reused})
		notice := content.ExportNotice(content.Kind(v.Kind), s.SameSourceExport(ctx, v))
		if v.Quality == "passed" {
			notice += "；检查通过不是已接受为正式版"
		}
		if !strings.Contains(out.Notice, notice) {
			if out.Notice != "" {
				out.Notice += " "
			}
			out.Notice += notice
		}
	}
	out.Complete = true
	return out, nil
}

func writeBundleFile(ctx context.Context, r *os.Root, name string, data []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") || len(data) > content.MaxInputBytes {
		return false, domain.ErrInvalid
	}
	verifyExisting := func() (bool, error) {
		existing, e := r.Open(name)
		if e != nil {
			return false, e
		}
		defer existing.Close()
		actual, e := io.ReadAll(io.LimitReader(existing, int64(len(data))+1))
		if e != nil {
			return false, e
		}
		if len(actual) != len(data) || digest(actual) != digest(data) {
			return false, errors.New("交付目录已有不同内容，请选择新的导出目录；现有文件未覆盖")
		}
		return true, nil
	}
	if _, err := r.Lstat(name); err == nil {
		return verifyExisting()
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	// Stage under the opened directory so a crash leaves only an ignorable
	// temporary file. Link publishes complete bytes without replacing a file
	// created by another writer between the existence check and publication.
	temp := ".office-export-" + ulid.Make().String() + ".tmp"
	f, err := r.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	defer func() { _ = r.Remove(temp) }()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if err = r.Link(temp, name); errors.Is(err, os.ErrExist) {
		return verifyExisting()
	} else if err != nil {
		return false, fmt.Errorf("导出目录无法安全发布完整文件，请选择本地磁盘目录：%w", err)
	}
	return false, nil
}
