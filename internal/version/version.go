package version

import (
	"runtime/debug"
	"strings"
)

// These values may be overwritten at link time. Business logic must not
// import this package to make decisions.
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

func Line() string {
	v, c, _ := current()
	return "codex-account " + v + " (" + c + ")"
}

func current() (string, string, string) {
	info, ok := debug.ReadBuildInfo()
	return resolve(Version, Commit, BuildTime, info, ok)
}

func resolve(version, commit, built string, info *debug.BuildInfo, ok bool) (string, string, string) {
	if !injected(version) && ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			version = v
		} else if rev := setting(info, "vcs.revision"); rev != "" {
			version = "devel-" + shortSHA(rev)
			if setting(info, "vcs.modified") == "true" {
				version += "-dirty"
			}
		}
	}
	if !injected(commit) && ok {
		if rev := setting(info, "vcs.revision"); rev != "" {
			commit = rev
		}
	}
	if !injected(built) && ok {
		if t := setting(info, "vcs.time"); t != "" {
			built = t
		}
	}
	if version == "" {
		version = "dev"
	}
	if commit == "" {
		commit = "none"
	}
	if built == "" {
		built = "unknown"
	}
	return version, commit, built
}

func injected(v string) bool {
	switch strings.TrimSpace(v) {
	case "", "dev", "none", "unknown":
		return false
	default:
		return true
	}
}

func setting(info *debug.BuildInfo, key string) string {
	if info == nil {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}

func shortSHA(rev string) string {
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}
