//go:build wago_lean

// Lean/TinyGo build: TinyGo cannot link net/http, so the registry commands are
// stubbed. Use a full wago binary to authenticate and publish to the registry.
// The command surface (names, flags, --help) is declared in cmd_registry.go and
// works here; only these Run bodies are unavailable. The credential store
// (registry_config.go) is net-free and still compiles, but nothing here reads it.

package main

func leanUnavailable(cmd string) {
	fatal("%s: registry commands need a full wago binary (this lean build cannot link net/http)", cmd)
}

func registryLogin(*Ctx)       { leanUnavailable("login") }
func registryLogout(*Ctx)      { leanUnavailable("logout") }
func registryWhoami(*Ctx)      { leanUnavailable("whoami") }
func registryPublish(*Ctx)     { leanUnavailable("publish") }
func registryUnpublish(*Ctx)   { leanUnavailable("unpublish") }
func registryDeprecate(*Ctx)   { leanUnavailable("deprecate") }
func registrySearch(*Ctx)      { leanUnavailable("search") }
func registryInfo(*Ctx)        { leanUnavailable("info") }
func registryVersions(*Ctx)    { leanUnavailable("versions") }
func registryStar(*Ctx)        { leanUnavailable("star") }
func registryUnstar(*Ctx)      { leanUnavailable("unstar") }
func registryTokenList(*Ctx)   { leanUnavailable("token list") }
func registryTokenCreate(*Ctx) { leanUnavailable("token create") }
func registryTokenRevoke(*Ctx) { leanUnavailable("token revoke") }
