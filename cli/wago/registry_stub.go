//go:build wago_lean

// Lean/TinyGo build: TinyGo cannot link net/http, so the registry commands are
// stubbed. Use a full wago binary to authenticate and publish to the registry.
// The credential store (registry_config.go) is net-free and still compiles here,
// but nothing in this build reads or writes it.

package main

func registryLogin(args []string) {
	fatal("login: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryLogout(args []string) {
	fatal("logout: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryWhoami(args []string) {
	fatal("whoami: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryPublish(args []string) {
	fatal("publish: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryUnpublish(args []string) {
	fatal("unpublish: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryDeprecate(args []string) {
	fatal("deprecate: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registrySearch(args []string) {
	fatal("search: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryInfo(args []string) {
	fatal("info: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryVersions(args []string) {
	fatal("versions: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryStar(args []string) {
	fatal("star: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryUnstar(args []string) {
	fatal("unstar: registry commands need a full wago binary (this lean build cannot link net/http)")
}

func registryToken(args []string) {
	fatal("token: registry commands need a full wago binary (this lean build cannot link net/http)")
}
