package ocrapp

import (
	"context"
	"strings"
)

func (s *Service) RefreshInstallState() {
	if s == nil {
		return
	}
	s.installMu.Lock()
	if s.installing {
		s.installMu.Unlock()
		return
	}
	s.installMu.Unlock()
	packRoot := ""
	if r, err := s.Routing(); err == nil {
		packRoot = s.resolvePackRoot(r.PackRoot)
	}
	if packRoot == "" && s.installer != nil {
		packRoot = s.installer.BundleDir(RuntimeID)
	}
	if !DetectPPOcrPack(packRoot).Available {
		return
	}
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if !s.installing {
		s.installState, s.lastInstallErr = "ready", ""
	}
}

func (s *Service) BeginInstall() {
	if s == nil {
		return
	}
	s.installMu.Lock()
	if s.installing {
		s.installMu.Unlock()
		return
	}
	s.installMu.Unlock()

	bundle := Runtime()
	outstanding := s.installer == nil || !s.installer.Installed(bundle)
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if s.installing {
		return
	}
	if !outstanding {
		pack := DetectPPOcrPack(s.installer.BundleDir(bundle.ID))
		if pack.Available {
			s.installState, s.lastInstallErr = "ready", ""
			return
		}
		s.installState, s.lastInstallErr = "failed", "PP-OCR 仅登记未接线，不能作为可执行引擎"
		return
	}
	s.installing, s.installState, s.lastInstallErr = true, "downloading", ""
	go s.runInstall(bundle)
}

func (s *Service) runInstall(bundle Bundle) {
	defer func() {
		s.installMu.Lock()
		s.installing = false
		s.installMu.Unlock()
	}()
	install := s.installBundle
	if install == nil {
		if s.installer == nil {
			s.installMu.Lock()
			s.installState, s.lastInstallErr = "failed", "OCR 安装器未装配"
			s.installMu.Unlock()
			return
		}
		install = s.installer.Install
	}
	err := install(context.Background(), bundle, func(p Progress) {
		s.installMu.Lock()
		s.progress = p
		s.installMu.Unlock()
	})
	s.installMu.Lock()
	defer s.installMu.Unlock()
	if err != nil {
		s.installState, s.lastInstallErr = "failed", err.Error()
		return
	}
	if s.installer != nil && !DetectPPOcrPack(s.installer.BundleDir(bundle.ID)).Available {
		s.installState, s.lastInstallErr = "failed", "PP-OCR 仅登记未接线，不能作为可执行引擎"
		return
	}
	s.installState, s.lastInstallErr = "ready", ""
	s.persistInstalledPackLocked()
}

func (s *Service) InstallSnapshot() map[string]any {
	if s == nil {
		return map[string]any{"state": "idle", "percent": 0, "doneBytes": 0, "totalBytes": Runtime().TotalBytes()}
	}
	s.installMu.Lock()
	defer s.installMu.Unlock()
	total := s.progress.Total
	if total == 0 {
		total = Runtime().TotalBytes()
	}
	state := s.installState
	if state == "" {
		state = "idle"
	}
	out := map[string]any{
		"state":      state,
		"percent":    s.progress.Percent(),
		"doneBytes":  s.progress.Done,
		"totalBytes": total,
	}
	if s.progress.File != "" {
		out["file"] = s.progress.File
	}
	if s.lastInstallErr != "" {
		out["lastError"] = installUserLastError(s.lastInstallErr)
	}
	return out
}

func installUserLastError(msg string) string {
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "ocr: "))
	if msg == "" {
		return ""
	}
	if containsHan(msg) {
		return truncateRunes(msg, 512)
	}
	if strings.Contains(msg, "digest mismatch") {
		return "下载文件校验失败，请重试"
	}
	if idx := strings.Index(msg, "HTTP "); idx >= 0 {
		code := ""
		for _, c := range msg[idx+5:] {
			if c >= '0' && c <= '9' {
				code += string(c)
				continue
			}
			break
		}
		if code != "" {
			return "下载失败（HTTP " + code + "）"
		}
	}
	return "下载失败，请检查网络后重试"
}

func (s *Service) persistInstalledPackLocked() {
	if s == nil || s.store == nil {
		return
	}
	root := ""
	if s.installer != nil {
		dir := s.installer.BundleDir(RuntimeID)
		if DetectPPOcrPack(dir).Available {
			root = dir
		}
	}
	if root == "" {
		return
	}
	cur, err := s.store.Get()
	if err != nil {
		return
	}
	if strings.TrimSpace(cur.PackRoot) == root {
		return
	}
	next := cur
	next.PackRoot = root
	if strings.TrimSpace(next.LocalEngine) == "" {
		next.LocalEngine = "auto"
	}
	_, _ = s.store.CompareAndSet(next, cur.Revision)
}

func containsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

func truncateRunes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
