package skillapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

// validateRunnableSkill checks the stored executable contract, not whether a
// model has successfully performed the intended business task.
func (s *Service) validateRunnableSkill(sk skill.Skill) error {
	if !allowlistedSkillEntryPoint(sk.EntryPoint) {
		return ErrUnknownEntryPoint
	}
	if !utf8.ValidString(sk.ManifestJSON) || len(sk.ManifestJSON) < 2 || len(sk.ManifestJSON) > 65536 {
		return errors.New("技能 manifest 必须是有效 UTF-8 JSON 对象，且不超过 64 KiB")
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal([]byte(sk.ManifestJSON), &manifest); err != nil || manifest == nil {
		return errors.New("技能 manifest 必须是有效 JSON 对象")
	}
	if markdownSkillEntryPoint(sk.EntryPoint) || strings.HasPrefix(sk.EntryPoint, "builtin://") {
		var prompt string
		if json.Unmarshal(manifest["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" || strings.ContainsRune(prompt, 0) {
			return errors.New("技能需要有效且非空的 manifest.prompt；尚未发布或执行")
		}
	}
	var refs []string
	if raw, exists := manifest["references"]; exists && json.Unmarshal(raw, &refs) != nil {
		return errors.New("技能 references 必须是相对路径数组")
	}
	for _, ref := range refs {
		if !PackageFilePath(ref) {
			return fmt.Errorf("技能 reference 路径无效：%s", ref)
		}
	}
	_, err := s.PackageFiles(sk)
	return err
}
