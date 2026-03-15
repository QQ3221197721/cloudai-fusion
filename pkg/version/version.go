// Package version provides build-time version information injected via ldflags.
//
// Build with:
//
//	go build -ldflags "-X github.com/cloudai-fusion/cloudai-fusion/pkg/version.Version=v0.2.0 \
//	  -X github.com/cloudai-fusion/cloudai-fusion/pkg/version.GitCommit=$(git rev-parse --short HEAD) \
//	  -X github.com/cloudai-fusion/cloudai-fusion/pkg/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
//	  -X github.com/cloudai-fusion/cloudai-fusion/pkg/version.GitTreeState=clean"
package version

import (
	"encoding/json"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// These variables are set at build time via -ldflags.
var (
	// Version is the semantic version (e.g., "v0.2.0").
	Version = "dev"

	// GitCommit is the short SHA of the git commit.
	GitCommit = "unknown"

	// GitTreeState indicates if the git tree was clean or dirty at build time.
	GitTreeState = "unknown"

	// BuildTime is the UTC timestamp of the build.
	BuildTime = "unknown"
)

// Info contains all version metadata.
type Info struct {
	Version      string `json:"version"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	BuildTime    string `json:"buildTime"`
	GoVersion    string `json:"goVersion"`
	Compiler     string `json:"compiler"`
	Platform     string `json:"platform"`
	GoModule     string `json:"goModule,omitempty"`
	GoChecksum   string `json:"goChecksum,omitempty"`
}

// Get returns the version information populated at build time.
func Get() Info {
	info := Info{
		Version:      Version,
		GitCommit:    GitCommit,
		GitTreeState: GitTreeState,
		BuildTime:    BuildTime,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}

	// Enrich from Go module build info if available
	if bi, ok := debug.ReadBuildInfo(); ok {
		info.GoModule = bi.Main.Path
		if bi.Main.Sum != "" {
			info.GoChecksum = bi.Main.Sum
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.GitCommit == "unknown" && s.Value != "" {
					info.GitCommit = truncate(s.Value, 12)
				}
			case "vcs.modified":
				if info.GitTreeState == "unknown" {
					if s.Value == "true" {
						info.GitTreeState = "dirty"
					} else {
						info.GitTreeState = "clean"
					}
				}
			case "vcs.time":
				if info.BuildTime == "unknown" && s.Value != "" {
					info.BuildTime = s.Value
				}
			}
		}
	}

	return info
}

// String returns a human-readable version string.
func (i Info) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Version:      %s\n", i.Version))
	sb.WriteString(fmt.Sprintf("Git Commit:   %s\n", i.GitCommit))
	sb.WriteString(fmt.Sprintf("Git State:    %s\n", i.GitTreeState))
	sb.WriteString(fmt.Sprintf("Build Time:   %s\n", i.BuildTime))
	sb.WriteString(fmt.Sprintf("Go Version:   %s\n", i.GoVersion))
	sb.WriteString(fmt.Sprintf("Compiler:     %s\n", i.Compiler))
	sb.WriteString(fmt.Sprintf("Platform:     %s\n", i.Platform))
	return sb.String()
}

// JSON returns the version information as a JSON string.
func (i Info) JSON() string {
	data, _ := json.MarshalIndent(i, "", "  ")
	return string(data)
}

// Short returns a compact one-line version string.
func (i Info) Short() string {
	return fmt.Sprintf("%s (%s/%s)", i.Version, i.GitCommit, i.Platform)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
