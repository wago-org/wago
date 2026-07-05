//go:build wago_lean

// Lean build: the version manager (which needs net/http) is compiled out to keep
// the size-optimized/TinyGo release binary small. `wago version` still reports the
// binary version; the management subcommands point at a full build.

package main

func versionCmd(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	fatal("version %s: version management is not built into this lean binary\n"+
		"(build without -tags wago_lean; use `wago --version` for the version number)", sub)
}
