package wagocli

// The plugin command surface: top-level `wago add`/`wago rm` for the common
// install/remove, and `wago plugin` for the rest (list, inspect, grant, update,
// publish). The consume side records dependencies in wago.json, `go get`s them
// into a generated .wago build module, and compiles a custom wago (see
// plugin_build.go / wagomodule.go). The publish side talks to the registry and is
// build-tagged (registry_net.go vs the lean registry_stub.go).

// addCommand is `wago add <plugin>`: install a plugin — record it in wago.json,
// rebuild wago with it, and review its capabilities.
func addCommand() *Cmd {
	return &Cmd{
		Name:    "add",
		Aliases: []string{"install", "i"},
		Summary: "add a plugin: record it in wago.json, rebuild wago, review its capabilities",
		Args:    "<module>[@version]",
		Flags:   []Flag{scopeGlobalFlag, scopeLocalFlag, forceFlag, verboseFlag},
		Run: func(c *Ctx) {
			pkgAdd(normalizeModuleRef(c.one("<module>[@version]")), pkgOpts{
				global:  resolveScope(c.Bool("global"), c.Bool("local")),
				force:   c.Bool("force"),
				verbose: c.Bool("verbose"),
			})
		},
	}
}

// rmCommand is `wago rm <plugin>`: remove a plugin from wago.json and rebuild.
func rmCommand() *Cmd {
	return &Cmd{
		Name:    "rm",
		Aliases: []string{"remove", "uninstall"},
		Summary: "remove a plugin from wago.json and rebuild",
		Args:    "<name>",
		Flags:   []Flag{scopeGlobalFlag, scopeLocalFlag},
		Run: func(c *Ctx) {
			pkgRemove(normalizeModuleRef(c.one("<name>")), resolveScope(c.Bool("global"), c.Bool("local")))
		},
	}
}

// pluginCommand is `wago plugin`: manage plugins beyond add/rm — list what's
// installed, inspect one, edit its capabilities, update, and publish your own.
func pluginCommand() *Cmd {
	jsonFlag := Flag{Name: "json", Bool: true, Help: "emit machine-readable JSON"}
	scope := func(c *Ctx) bool { return resolveScope(c.Bool("global"), c.Bool("local")) }
	return &Cmd{
		Name:    "plugin",
		Aliases: []string{"plugins"},
		Summary: "manage plugins: list, inspect, grant, update, publish",
		Children: []*Cmd{
			{
				Name: "list", Aliases: []string{"ls"},
				Summary: "list the plugins installed in this wago",
				Flags:   []Flag{jsonFlag},
				Run:     func(c *Ctx) { pluginList(c.Bool("json")) },
			},
			{
				Name:    "inspect",
				Summary: "show a plugin's imports and capabilities",
				Args:    "<name>",
				Flags:   []Flag{jsonFlag},
				Run:     func(c *Ctx) { pluginInspect(c.one("<name>"), c.Bool("json")) },
			},
			{
				Name:    "grant",
				Summary: "review and edit a plugin's granted capabilities",
				Args:    "<name>",
				Flags:   []Flag{scopeGlobalFlag},
				Run:     func(c *Ctx) { pkgGrant(c.one("<name>"), c.Bool("global")) },
			},
			{
				Name: "update", Aliases: []string{"up", "upgrade"},
				Summary: "update plugins to their latest versions, then rebuild",
				Args:    "[module]",
				Flags:   []Flag{scopeGlobalFlag, scopeLocalFlag, verboseFlag},
				Run: func(c *Ctx) {
					pkgUpdate(normalizeModuleRef(c.opt("[module]")), pkgOpts{global: scope(c), verbose: c.Bool("verbose")})
				},
			},
			{
				Name:    "publish",
				Summary: "publish a plugin from wago.json",
				Flags: []Flag{
					{Name: "manifest", Arg: "<p>", Help: "manifest path (default wago.json)"},
					{Name: "commit", Arg: "<c>", Help: "commit SHA (default: git HEAD)"},
					{Name: "notes", Arg: "<s>", Help: "release notes"},
					{Name: "category", Arg: "<c>", Help: "plugin category"},
					{Name: "tags", Arg: "<a,b>", Help: "comma-separated tags"},
				},
				Run: registryPublish,
			},
			{
				Name:    "unpublish",
				Summary: "remove a published plugin or one version",
				Args:    "<name>[@version]",
				Flags:   []Flag{{Name: "yes", Bool: true, Help: "skip the confirmation prompt"}},
				Run:     registryUnpublish,
			},
			{
				Name:    "deprecate",
				Summary: "deprecate a plugin/version",
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

// Shared scope/build flags for add/rm/plugin.
var (
	scopeGlobalFlag = Flag{Name: "global", Short: "g", Bool: true, Help: "use the CLI-wide plugin set (~/.wago); default when the cwd has no wago.json"}
	scopeLocalFlag  = Flag{Name: "local", Short: "l", Bool: true, Help: "use this project's wago.json (create one here if absent)"}
	forceFlag       = Flag{Name: "force", Short: "f", Bool: true, Help: "ignore the build cache / fetch the latest version"}
	verboseFlag     = Flag{Name: "verbose", Short: "v", Bool: true, Help: "stream the underlying go output"}
)
