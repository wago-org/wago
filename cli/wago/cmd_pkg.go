package main

// pkgCommand is the `wago pkg` group: the package lifecycle, from declaring a
// dependency to publishing your own. The consume side (install/uninstall/list/
// build) manages the wago-plugins.json manifest for a custom build and is
// net-free; the publish side (publish/unpublish/deprecate) talks to the registry
// and is build-tagged (registry_net.go vs the lean registry_stub.go).
func pkgCommand() *Cmd {
	return &Cmd{
		Name:    "pkg",
		Summary: "install, build, and publish registry packages",
		Children: []*Cmd{
			{
				Name: "install", Aliases: []string{"add"},
				Summary: "declare a package in wago-plugins.json",
				Args:    "<module>",
				Flags: []Flag{
					{Name: "name", Arg: "<name>", Help: "package name (defaults to one derived from the module)"},
					{Name: "version", Arg: "<v>", Help: "pin a module version"},
				},
				Run: func(c *Ctx) { pluginManifestAdd(c.one("<module>"), c.Str("name"), c.Str("version")) },
			},
			{
				Name: "uninstall", Aliases: []string{"remove", "rm"},
				Summary: "remove a package from the manifest",
				Args:    "<name>",
				Run:     func(c *Ctx) { pluginManifestRemove(c.one("<name>")) },
			},
			{
				Name: "list", Aliases: []string{"ls"},
				Summary: "list declared packages",
				Run:     func(*Ctx) { pluginManifestShow() },
			},
			{
				Name:    "build",
				Summary: "build a custom wago binary with the manifest's plugins",
				Run:     func(*Ctx) { pluginBuild() },
			},
			{
				Name:    "publish",
				Summary: "publish a package from wago.json",
				Flags: []Flag{
					{Name: "manifest", Arg: "<p>", Help: "manifest path (default wago.json)"},
					{Name: "version", Arg: "<v>", Help: "version to publish (default: wago.json version, else newest git tag)"},
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
