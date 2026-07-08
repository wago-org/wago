package main

// pluginCommand is the `wago plugin` group: inspect the plugins compiled into this
// binary and manage the wago-plugins.json manifest for a custom build. The leaf
// actions (pluginList, pluginInspect, pluginManifest*) live in plugins.go /
// manifest.go; this file only declares the command surface.
func pluginCommand() *Cmd {
	jsonFlag := Flag{Name: "json", Bool: true, Help: "emit machine-readable JSON"}
	return &Cmd{
		Name:       "plugin",
		Aliases:    []string{"plugins"},
		Summary:    "inspect compiled-in plugins and manage the manifest",
		DefaultSub: "list",
		Children: []*Cmd{
			{
				Name: "list", Aliases: []string{"ls"},
				Summary: "list plugins compiled into this binary",
				Flags:   []Flag{jsonFlag},
				Run:     func(c *Ctx) { pluginList(c.Bool("json")) },
			},
			{
				Name: "inspect", Aliases: []string{"show"},
				Summary: "show a plugin's imports and capabilities",
				Args:    "<name>",
				Flags:   []Flag{jsonFlag},
				Run:     func(c *Ctx) { pluginInspect(c.one("<name>"), c.Bool("json")) },
			},
			{
				Name: "add", Aliases: []string{"install"},
				Summary: "declare a plugin in wago-plugins.json",
				Args:    "<module>",
				Flags: []Flag{
					{Name: "name", Arg: "<name>", Help: "plugin name (defaults to one derived from the module)"},
					{Name: "version", Arg: "<v>", Help: "pin a module version"},
				},
				Run: func(c *Ctx) { pluginManifestAdd(c.one("<module>"), c.Str("name"), c.Str("version")) },
			},
			{
				Name: "remove", Aliases: []string{"uninstall", "rm"},
				Summary: "remove a plugin from the manifest",
				Args:    "<name>",
				Run:     func(c *Ctx) { pluginManifestRemove(c.one("<name>")) },
			},
			{
				Name: "manifest", Aliases: []string{"declared"},
				Summary: "show declared plugins (for a custom build)",
				Run:     func(*Ctx) { pluginManifestShow() },
			},
			{
				Name:    "build",
				Summary: "preview the custom build described by the manifest",
				Run:     func(*Ctx) { pluginBuild() },
			},
		},
	}
}
