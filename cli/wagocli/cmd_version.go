package wagocli

import "github.com/wago-org/wago"

// versionCommand is the `wago version` manager: list/use/install wago toolchain
// versions. The binary's own version is printed by `wago --version`. Management
// (list/use/current/which/uninstall) is net-free; the downloader (install,
// update, list-remote) uses Go's HTTP client in the standard build and the host
// curl executable in the lean build (version_net.go vs version_net_stub.go).
func versionCommand() *Cmd {
	dirs := func() wago.Dirs { return wago.DirsFor(versionString()) }
	return &Cmd{
		Name:    "version",
		Summary: "manage installed toolchain versions (list, use, install, …)",
		Children: []*Cmd{
			{
				Name: "list", Aliases: []string{"ls"},
				Summary: "list installed versions",
				Run:     func(*Ctx) { vmList(dirs()) },
			},
			{
				Name:    "current",
				Summary: "print the active version",
				Run:     func(*Ctx) { vmCurrent(dirs()) },
			},
			{
				Name:    "which",
				Summary: "print the path to the active binary",
				Run:     func(*Ctx) { vmWhich(dirs()) },
			},
			{
				Name:    "switch",
				Aliases: []string{"swap"},
				Summary: "select an installed version (interactive with no argument)",
				Args:    "[version]",
				Run: func(c *Ctx) {
					if len(c.Args) == 0 {
						vmChooseInstalled(dirs())
						return
					}
					vmUse(dirs(), c.one("[version]"))
				},
			},
			{
				Name: "install", Aliases: []string{"add"},
				Summary: "install a pinned version, release channel, or latest",
				Args:    "[version]",
				Flags:   []Flag{{Name: "latest", Bool: true, Help: "install the latest release"}, {Name: "nightly", Bool: true, Help: "install nightly"}, {Name: "canary", Bool: true, Help: "install the latest canary"}},
				Run: func(c *Ctx) {
					vmInstallRequested(dirs(), c.Args, c.Bool("latest"), c.Bool("nightly"), c.Bool("canary"))
				},
			},
			{
				Name:    "update",
				Summary: "refresh an installed version or release channel",
				Args:    "[version]",
				Flags: []Flag{
					{Name: "nightly", Bool: true, Help: "refresh the latest nightly release"},
					{Name: "canary", Bool: true, Help: "refresh the canary built from main"},
				},
				Run: func(c *Ctx) {
					ver, err := updateVersionTarget(activeVersion(dirs()), c.Args, c.Bool("nightly"), c.Bool("canary"))
					if err != nil {
						fatal("version update: %v", err)
					}
					vmUpdate(dirs(), ver)
				},
			},
			{
				Name: "uninstall", Aliases: []string{"remove", "rm"},
				Summary: "remove an installed version",
				Args:    "<version>",
				Run:     func(c *Ctx) { vmUninstall(dirs(), c.one("<version>")) },
			},
			{
				Name: "list-remote", Aliases: []string{"ls-remote"},
				Summary: "list versions available to download",
				Run:     func(*Ctx) { vmListRemote() },
			},
		},
	}
}
