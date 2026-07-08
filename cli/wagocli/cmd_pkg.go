package wagocli

// pkgCommand is the `wago pkg` group: the package lifecycle, from declaring a
// dependency to publishing your own. The consume side (add/remove/list/build)
// records plugin dependencies in wago.json, `go get`s them into a generated .wago
// build module, and compiles a custom wago from them (see plugin_build.go /
// wagomodule.go). The publish side (publish/unpublish/deprecate) talks to the
// registry and is build-tagged (registry_net.go vs the lean registry_stub.go).
func pkgCommand() *Cmd {
	global := Flag{Name: "global", Bool: true, Help: "operate on the CLI-wide plugin set (~/.wago) instead of this project"}
	return &Cmd{
		Name:    "pkg",
		Summary: "add, build, and publish registry packages",
		Children: []*Cmd{
			{
				Name: "add", Aliases: []string{"install"},
				Summary: "add a plugin dependency (wago.json + go get)",
				Args:    "<module>",
				Flags:   []Flag{global},
				Run:     func(c *Ctx) { pkgAdd(c.one("<module>"), c.Bool("global")) },
			},
			{
				Name: "remove", Aliases: []string{"uninstall", "rm"},
				Summary: "remove a plugin dependency",
				Args:    "<name>",
				Flags:   []Flag{global},
				Run:     func(c *Ctx) { pkgRemove(c.one("<name>"), c.Bool("global")) },
			},
			{
				Name: "list", Aliases: []string{"ls"},
				Summary: "list declared plugin dependencies",
				Flags:   []Flag{global},
				Run:     func(c *Ctx) { pkgList(c.Bool("global")) },
			},
			{
				Name:    "build",
				Summary: "build a custom wago binary with the declared plugins",
				Flags:   []Flag{global},
				Run:     func(c *Ctx) { pkgBuild(c.Bool("global")) },
			},
			{
				Name:    "status",
				Aliases: []string{"which"},
				Summary: "show the running engine and the active plugin set (global vs local)",
				Run:     func(*Ctx) { pkgStatus() },
			},
			{
				Name:    "publish",
				Summary: "publish a package from wago.json",
				Flags: []Flag{
					{Name: "manifest", Arg: "<p>", Help: "manifest path (default wago.json)"},
					{Name: "commit", Arg: "<c>", Help: "commit SHA (default: git HEAD)"},
					{Name: "notes", Arg: "<s>", Help: "release notes"},
					{Name: "category", Arg: "<c>", Help: "package category"},
					{Name: "tags", Arg: "<a,b>", Help: "comma-separated tags"},
				},
				Run: registryPublish,
			},
			{
				Name:    "unpublish",
				Summary: "remove a package or one version",
				Args:    "<name>[@version]",
				Flags:   []Flag{{Name: "yes", Bool: true, Help: "skip the confirmation prompt"}},
				Run:     registryUnpublish,
			},
			{
				Name:    "deprecate",
				Summary: "deprecate a package/version",
				Args:    "<name>[@version]",
				Flags: []Flag{
					{Name: "message", Arg: "<m>", Help: "deprecation notice"},
					{Name: "undo", Bool: true, Help: "reverse a deprecation"},
				},
				Run: registryDeprecate,
			},
		},
	}
}
