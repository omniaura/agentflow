package cfg

import (
	"encoding/json"
	"os"
	"runtime/debug"
	"testing"
)

func TestReleaseVersionMatchesManifest(t *testing.T) {
	data, err := os.ReadFile("../.release-please-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["."] != releaseVersion {
		t.Fatalf("release-please manifest is %q but cfg releaseVersion is %q", manifest["."], releaseVersion)
	}
}

func TestResolveVersion(t *testing.T) {
	const sum = "h1:abc="
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"no build info", nil, releaseVersion},
		{"local checkout", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}, releaseVersion},
		{"local checkout with vcs pseudo-version", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v0.10.1-0.20261021150000-abcdef123456+dirty"}}, releaseVersion},
		{"go run at release", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v0.11.0", Sum: sum}}, "0.11.0"},
		{"go tool dependency at release", &debug.BuildInfo{
			Main: debug.Module{Path: "example.com/app", Version: "(devel)"},
			Deps: []*debug.Module{{Path: "golang.org/x/sync", Version: "v0.20.0", Sum: sum}, {Path: modulePath, Version: "v0.11.0", Sum: sum}},
		}, "0.11.0"},
		{"go tool dependency at pseudo-version", &debug.BuildInfo{
			Main: debug.Module{Path: "example.com/app"},
			Deps: []*debug.Module{{Path: modulePath, Version: "v0.10.1-0.20260913231947-6291c9b4f95c", Sum: sum}},
		}, "0.10.1-0.20260913231947-6291c9b4f95c"},
		{"replaced dependency", &debug.BuildInfo{
			Main: debug.Module{Path: "example.com/app"},
			Deps: []*debug.Module{{Path: modulePath, Version: "v0.11.0", Replace: &debug.Module{Path: "../agentflow"}}},
		}, releaseVersion},
		{"agentflow not in build", &debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}}, releaseVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.info); got != tt.want {
				t.Fatalf("resolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
