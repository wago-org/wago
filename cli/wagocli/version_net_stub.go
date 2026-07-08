//go:build wago_lean

// Lean/TinyGo build: TinyGo cannot link net/http, so the downloader is stubbed.
// Version management (list/use/current/which/uninstall) still works — this build
// just can't fetch new versions over the network. Install a binary manually into
// the versions directory (see `wago env`), or use a full wago binary.

package wagocli

import "github.com/wago-org/wago"

func vmInstall(d wago.Dirs, ver string) {
	fatal("version install: downloading is not available in this lean binary "+
		"(TinyGo cannot link net/http).\n"+
		"Place the binary at %s manually, then `wago version use %s` — or use a full wago binary.",
		d.VersionBinary(ver), ver)
}

func vmListRemote() {
	fatal("version list-remote: not available in this lean binary (TinyGo cannot link net/http); use a full wago binary")
}
