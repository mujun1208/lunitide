package skillarchive

// ParseDocument shares the strict SKILL.md format used by pinned GitHub
// imports with the local package upload entry point.
func ParseDocument(body []byte) (name, description, prompt, license string, err error) {
	return parseSkill(body)
}
