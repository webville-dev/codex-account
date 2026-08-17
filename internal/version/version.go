package version

// These values may be overwritten at link time. Business logic must not
// import this package to make decisions.
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

func Line() string {
	return "codex-account " + Version + " (" + Commit + ")"
}
