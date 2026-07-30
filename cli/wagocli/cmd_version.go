package wagocli

import "github.com/wago-org/wago/internal/wagopaths"

// versionCommand is the `wago version` manager: list/switch/install wago toolchain
// versions. The binary's own version is printed by `wago --version`. Management
// (list/use/current/which/uninstall) is net-free; install and update use Go's
// HTTP client in the manager and the host curl executable only in legacy lean
// runtime builds.
func versionCommand() *Cmd {
	dirs := func() wagopaths.Dirs { return wagopaths.DirsFor(versionString()) }
	return &Cmd{
		Name:    "version",
		Summary: "manage Wago toolchain versions (list, switch, install, …)",
		Children: []*Cmd{
			{
				Name: "list", Aliases: []string{"ls", "list-installed", "ls-installed", "list-local"},
				Summary: "list locally installed versions",
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
				Flags: []Flag{
					{Name: "profile", Arg: "<name>", Help: "standard, lite, or minimal"},
					{Name: "build", Arg: "<name>", Help: "normal or tiny"},
				},
				Run: func(c *Ctx) {
					if len(c.Args) == 0 {
						vmChooseInstalled(dirs())
						return
					}
					profile := activeProfile(dirs())
					build := activeBuild(dirs())
					var err error
					if c.Str("profile") != "" {
						profile, err = wagopaths.ParseProfile(c.Str("profile"))
					}
					if err != nil {
						fatal("version switch: %v", err)
					}
					if c.Str("build") != "" {
						build, err = wagopaths.ParseBuild(c.Str("build"))
					}
					if err != nil {
						fatal("version switch: %v", err)
					}
					if _, _, _, ok := installedRuntime(dirs(), c.one("[version]"), profile, build); !ok {
						vmInstallForSwitch(dirs(), c.one("[version]"), profile, build)
					}
					vmUse(dirs(), c.one("[version]"), profile, build)
				},
			},
			{
				Name: "install", Aliases: []string{"add"},
				Summary: "install a pinned version, release channel, or latest",
				Args:    "[version]",
				Flags: []Flag{
					{Name: "latest", Bool: true, Help: "install the latest release"},
					{Name: "nightly", Bool: true, Help: "install nightly"},
					{Name: "canary", Bool: true, Help: "install the latest canary"},
					{Name: "profile", Arg: "<name>", Help: "standard, lite, or minimal"},
					{Name: "build", Arg: "<name>", Help: "normal or tiny"},
				},
				Run: func(c *Ctx) {
					vmInstallRequested(dirs(), c.Args, c.Bool("latest"), c.Bool("nightly"), c.Bool("canary"), c.Str("profile"), c.Str("build"))
				},
			},
			{
				Name:    "update",
				Summary: "refresh an installed version or release channel",
				Args:    "[version]",
				Flags: []Flag{
					{Name: "nightly", Bool: true, Help: "refresh the latest nightly release"},
					{Name: "canary", Bool: true, Help: "refresh the canary built from main"},
					{Name: "profile", Arg: "<name>", Help: "profile to refresh (default active)"},
					{Name: "build", Arg: "<name>", Help: "normal or tiny (default active)"},
				},
				Run: func(c *Ctx) {
					ver, err := updateVersionTarget(activeVersion(dirs()), c.Args, c.Bool("nightly"), c.Bool("canary"))
					if err != nil {
						fatal("version update: %v", err)
					}
					profile := activeProfile(dirs())
					build := activeBuild(dirs())
					if value := c.Str("profile"); value != "" {
						var parseErr error
						profile, parseErr = wagopaths.ParseProfile(value)
						if parseErr != nil {
							fatal("version update: %v", parseErr)
						}
					}
					if value := c.Str("build"); value != "" {
						var parseErr error
						build, parseErr = wagopaths.ParseBuild(value)
						if parseErr != nil {
							fatal("version update: %v", parseErr)
						}
					}
					vmUpdate(dirs(), ver, profile, build)
				},
			},
			{
				Name: "uninstall", Aliases: []string{"remove", "rm"},
				Summary: "remove an installed version",
				Args:    "<version>",
				Run:     func(c *Ctx) { vmUninstall(dirs(), c.one("<version>")) },
			},
		},
	}
}
