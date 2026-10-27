package gonames

import "testing"

func TestCleanPackageName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercases plain names",
			input: "Agentflow",
			want:  "agentflow",
		},
		{
			name:  "removes spaces hyphens and underscores",
			input: "Agent Flow-cli_name",
			want:  "agentflowcliname",
		},
		{
			name:  "drops other punctuation",
			input: "pkg.name/v2",
			want:  "pkgnamev2",
		},
		{
			name:  "handles empty strings",
			input: "",
			want:  "p",
		},
		{name: "leading digit", input: "003", want: "p003"},
		{name: "keyword", input: "func", want: "pfunc"},
		{name: "dots", input: "v1.2", want: "v12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanPackageName(tt.input); got != tt.want {
				t.Fatalf("CleanPackageName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
