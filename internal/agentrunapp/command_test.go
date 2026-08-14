package agentrunapp

import "testing"

func TestCommandCwdAllowedRequiresCwdInsideExecuteScope(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		cwd      string
		want     bool
	}{
		{name: "root denied by restricted grant", patterns: []string{"safe/**"}, cwd: "", want: false},
		{name: "unrelated directory denied", patterns: []string{"safe/**"}, cwd: "restricted", want: false},
		{name: "ancestor denied", patterns: []string{"project/safe/**"}, cwd: "project", want: false},
		{name: "granted subtree root allowed", patterns: []string{"safe/**"}, cwd: "safe", want: true},
		{name: "granted descendant allowed", patterns: []string{"safe/**"}, cwd: "safe/project", want: true},
		{name: "global grant allows root", patterns: []string{"**"}, cwd: "", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandCwdAllowed(tt.patterns, tt.cwd); got != tt.want {
				t.Fatalf("commandCwdAllowed(%v, %q)=%v want %v", tt.patterns, tt.cwd, got, tt.want)
			}
		})
	}
}
