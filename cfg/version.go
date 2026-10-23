package cfg

import (
	"runtime/debug"
	"strings"
)

// releaseVersion is the latest release, stamped by release-please. It is the
// fallback for builds from a local checkout.
const releaseVersion = "0.12.0" // x-release-please-version

const modulePath = "github.com/omniaura/agentflow"

// Version is the agentflow version without a leading "v". GoReleaser sets it
// with -ldflags "-X github.com/omniaura/agentflow/cfg.Version=..."; otherwise
// it is resolved from the build info.
var Version string

func init() {
	if Version == "" {
		info, _ := debug.ReadBuildInfo()
		Version = resolveVersion(info)
	}
}

// resolveVersion reports the version of the agentflow module in info: the
// main module for go install/go run pkg@version, or a dependency for
// go tool af. Pseudo-versions are kept as-is. Local checkouts and replaced
// modules have no downloaded version and fall back to releaseVersion.
func resolveVersion(info *debug.BuildInfo) string {
	if info == nil {
		return releaseVersion
	}
	mod := &info.Main
	if mod.Path != modulePath {
		mod = nil
		for _, dep := range info.Deps {
			if dep.Path == modulePath {
				mod = dep
				break
			}
		}
	}
	if mod == nil || mod.Sum == "" || mod.Replace != nil {
		return releaseVersion
	}
	return strings.TrimPrefix(mod.Version, "v")
}
