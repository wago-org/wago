package wagocli

// moduleCommand is the `wago module` group: inspect a module's imports and the
// capabilities it requires, resolved against the compiled-in plugins. The leaf
// actions live in module.go.
func moduleCommand() *Cmd {
	return &Cmd{
		Name:    "module",
		Aliases: []string{"mod"},
		Summary: "inspect a module's imports and required capabilities",
		Children: []*Cmd{
			{
				Name:    "imports",
				Summary: "list a module's imports (resolved vs plugins)",
				Args:    "<file>",
				Run:     func(c *Ctx) { moduleImports(c.one("<file>")) },
			},
			{
				Name: "capabilities", Aliases: []string{"caps"},
				Summary: "list the capabilities a module requires",
				Args:    "<file>",
				Run:     func(c *Ctx) { moduleCapabilities(c.one("<file>")) },
			},
		},
	}
}
