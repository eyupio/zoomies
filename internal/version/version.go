// Package version carries build metadata stamped in at link time.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// These are overridden with -ldflags "-X github.com/eyupio/zoomies/internal/version.Version=..."
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		Commit, Date = fill(Commit, Date, info.Settings)
	}
}

// fill decides the commit and the date, taking each from the link-time stamp
// when there is one and from the build information Go embeds otherwise.
//
// Each independently, which is the point: this used to give up entirely the
// moment a commit had been stamped, so a build that passed one and not the
// other -- the container image, which had no DATE build argument -- reported
// no date at all, and `zoomies version` on a compose install was quieter than
// the same release installed natively.
func fill(commit, date string, settings []debug.BuildSetting) (string, string) {
	if commit != "" && date != "" {
		return commit, date
	}
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
			}
		case "vcs.time":
			if date == "" {
				date = s.Value
			}
		}
	}
	return commit, date
}

// Short returns a compact "v1.2.3 (abc1234)" style identifier.
func Short() string {
	if len(Commit) >= 7 {
		return fmt.Sprintf("%s (%s)", Version, Commit[:7])
	}
	return Version
}

// String returns the full human readable version banner line.
func String() string {
	return fmt.Sprintf("zoomies %s %s/%s %s", Short(), runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// UserAgent is sent on every outbound GitHub API call.
func UserAgent() string { return "zoomies/" + Version }
