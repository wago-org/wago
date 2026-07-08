package main

// pluginCommand is the `wago plugin` group: inspect the plugins compiled into
// this binary. Declaring/installing packages for a custom build lives under
// `wago pkg`. The leaf actions (pluginList, pluginInspect) are in plugins.go.
func pluginCommand() *Cmd {
	jsonFlag := Flag{Name: "json", Bool: true, Help: "emit machine-readable JSON"}
	return &Cmd{
		Name:       "plugin",
		Aliases:    []string{"plugins"},
		Summary:    "inspect plugins compiled into this binary",
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
		},
	}
}
