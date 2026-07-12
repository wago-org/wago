//go:build wago_lean

// Lean/TinyGo build: TinyGo cannot link net/http, so the registry-facing commands
// (wago auth *, wago plugin publish/unpublish/deprecate) are stubbed. Use a full wago
// binary to authenticate and publish. The command surface (names, flags, --help)
// is declared in cmd_auth.go / cmd_pkg.go and works here; only these Run bodies
// are unavailable. The credential store (registry_config.go) is net-free and still
// compiles, but nothing here reads it.

package wagocli

import "errors"

func resolveRegistryModule(string) (string, error) {
	return "", errors.New("resolving a plugin name needs a full wago binary; pass the full module path")
}

func leanUnavailable(cmd string) {
	fatal("%s: registry commands need a full wago binary (this lean build cannot link net/http)", cmd)
}

func registryLogin(*Ctx)     { leanUnavailable("auth login") }
func registryLogout(*Ctx)    { leanUnavailable("auth logout") }
func registryWhoami(*Ctx)    { leanUnavailable("auth whoami") }
func registryPublish(*Ctx)   { leanUnavailable("plugin publish") }
func registryUnpublish(*Ctx) { leanUnavailable("plugin unpublish") }
func registryDeprecate(*Ctx) { leanUnavailable("plugin deprecate") }
