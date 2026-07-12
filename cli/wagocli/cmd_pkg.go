package wagocli

// pkgCommand is the `wago pkg` group: the package lifecycle, from declaring a
// dependency to publishing your own. The consume side (add/remove/list/build)
// records plugin dependencies in wago.json, `go get`s them into a generated .wago
// build module, and compiles a custom wago from them (see plugin_build.go /
// wagomodule.go). The publish side (publish/unpublish/deprecate) talks to the
// registry and is build-tagged (registry_net.go vs the lean registry_stub.go).
func pkgCommand() *Cmd {
	global := Flag{Name: "global", Short: "g", Bool: true, Help: "force the CLI-wide package set (~/.wago); default when the cwd has no wago.json"}
	local := Flag{Name: "local", Short: "l", Bool: true, Help: "force this project's wago.json (create one here if absent)"}
	force := Flag{Name: "force", Short: "f", Bool: true, Help: "ignore the build cache / fetch the latest version"}
	verbose := Flag{Name: "verbose", Short: "v", Bool: true, Help: "stream the underlying go output"}
	jsonFlag := Flag{Name: "json", Bool: true, Help: "emit machine-readable JSON"}
	opts := func(c *Ctx) pkgOpts {
		return pkgOpts{global: resolveScope(c.Bool("global"), c.Bool("local")), force: c.Bool("force"), verbose: c.Bool("verbose")}
	}
	scope := func(c *Ctx) bool { return resolveScope(c.Bool("global"), c.Bool("local")) }
	return &Cmd{
		Name:    "pkg",
		Aliases: []string{"package"},
		Summary: "install, build, publish, and inspect packages",
		Children: []*Cmd{
			{
				Name: "install", Aliases: []string{"i"},
				Summary: "install a package: record it in wago.json, rebuild wago, review its capabilities",
				Args:    "<module>[@version]",
				Flags:   []Flag{global, local, force, verbose},
				Run:     func(c *Ctx) { pkgAdd(normalizeModuleRef(c.one("<module>[@version]")), opts(c)) },
			},
			{
				Name: "uninstall", Aliases: []string{"rm"},
				Summary: "uninstall a package: drop it from wago.json and rebuild",
				Args:    "<name>",
				Flags:   []Flag{global, local},
				Run:     func(c *Ctx) { pkgRemove(normalizeModuleRef(c.one("<name>")), scope(c)) },
			},
			{
				Name: "update", Aliases: []string{"up", "upgrade"},
				Summary: "update package dependencies to their latest versions, then rebuild",
				Args:    "[module]",
				Flags:   []Flag{global, local, verbose},
				Run:     func(c *Ctx) { pkgUpdate(normalizeModuleRef(c.opt("[module]")), opts(c)) },
			},
			{
				Name: "list", Aliases: []string{"ls"},
				Summary: "list declared package dependencies",
				Flags:   []Flag{global, local},
				Run:     func(c *Ctx) { pkgList(scope(c)) },
			},
			{
				Name:    "build",
				Summary: "build a custom wago binary with the declared packages",
				Flags:   []Flag{global, local, force, verbose},
				Run:     func(c *Ctx) { pkgBuild(opts(c)) },
			},
			{
				Name:    "grant",
				Summary: "review and edit a package's granted capabilities",
				Args:    "<name>",
				Flags:   []Flag{global},
				Run:     func(c *Ctx) { pkgGrant(c.one("<name>"), c.Bool("global")) },
			},
			{
				Name: "info", Aliases: []string{"show", "view"},
				Summary: "show a package's registry info",
				Args:    "<name>",
				Run:     func(c *Ctx) { pkgInfo(c.one("<name>")) },
			},
			{
				Name:    "status",
				Aliases: []string{"which"},
				Summary: "show the running engine and the active package set (global vs local)",
				Run:     func(*Ctx) { pkgStatus() },
			},
			{
				Name: "compiled", Aliases: []string{"builtin"},
				Summary: "list packages compiled into this binary",
				Flags:   []Flag{jsonFlag},
				Run:     func(c *Ctx) { pluginList(c.Bool("json")) },
			},
			{
				Name:    "inspect",
				Summary: "show a compiled-in package's imports and capabilities",
				Args:    "<name>",
				Flags:   []Flag{jsonFlag},
				Run:     func(c *Ctx) { pluginInspect(c.one("<name>"), c.Bool("json")) },
			},
			{
				Name:    "plan",
				Summary: "validate and show the resolved package load order",
				Flags:   []Flag{jsonFlag},
				Run:     func(c *Ctx) { pluginPlan(c.Bool("json")) },
			},
			{
				Name:    "check",
				Summary: "validate package grants, services, config, and ordering",
				Run:     func(c *Ctx) { pluginCheck() },
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
