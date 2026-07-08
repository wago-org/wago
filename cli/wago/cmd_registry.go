package main

// Registry command surface for the wago registry at pkg.wago.sh. This file is
// unconditional so `wago publish --help` (and every other registry command's help)
// works in the lean build too — the Run bodies live in registry_net.go for the
// full build and registry_stub.go (fatal stubs) for the lean/TinyGo build.
func registryCommands() []*Cmd {
	jsonFlag := Flag{Name: "json", Bool: true, Help: "emit machine-readable JSON"}
	return []*Cmd{
		{
			Name: "login", Group: "registry",
			Summary: "authenticate to the registry (pkg.wago.sh)",
			Flags: []Flag{
				{Name: "link", Bool: true, Help: "log in via a browser link on this machine"},
				{Name: "code", Bool: true, Help: "log in with a one-time code (headless/remote)"},
				{Name: "token", Arg: "<t>", Help: "use this API token directly"},
				{Name: "with-token", Bool: true, Help: "read an API token from stdin (for CI)"},
			},
			Long: "With no flag, login asks whether to use a browser link or a one-time code.",
			Run:  registryLogin,
		},
		{
			Name: "logout", Group: "registry",
			Summary: "remove stored registry credentials",
			Run:     registryLogout,
		},
		{
			Name: "whoami", Group: "registry",
			Summary: "print the logged-in registry account",
			Run:     registryWhoami,
		},
		{
			Name: "search", Group: "registry",
			Summary: "search packages",
			Args:    "<query…>",
			Flags:   []Flag{{Name: "limit", Arg: "<n>", Help: "max rows to show (default 20)"}, jsonFlag},
			Run:     registrySearch,
		},
		{
			Name: "info", Group: "registry",
			Summary: "show a package's details",
			Args:    "<pkg>",
			Flags:   []Flag{jsonFlag},
			Run:     registryInfo,
		},
		{
			Name: "versions", Group: "registry",
			Summary: "list a package's published versions",
			Args:    "<pkg>",
			Flags:   []Flag{jsonFlag},
			Run:     registryVersions,
		},
		{
			Name: "star", Group: "registry",
			Summary: "star a package",
			Args:    "<pkg>",
			Run:     registryStar,
		},
		{
			Name: "unstar", Group: "registry",
			Summary: "remove a star from a package",
			Args:    "<pkg>",
			Run:     registryUnstar,
		},
		{
			Name: "token", Group: "registry",
			Summary: "manage API tokens",
			Children: []*Cmd{
				{
					Name: "list", Aliases: []string{"ls"},
					Summary: "list your API tokens",
					Run:     registryTokenList,
				},
				{
					Name: "create", Aliases: []string{"new"},
					Summary: "mint a new API token",
					Flags:   []Flag{{Name: "label", Arg: "<l>", Help: "a label for the token"}},
					Run:     registryTokenCreate,
				},
				{
					Name: "revoke", Aliases: []string{"rm", "delete"},
					Summary: "revoke an API token by id",
					Args:    "<id>",
					Run:     registryTokenRevoke,
				},
			},
		},
		{
			Name: "publish", Group: "registry",
			Summary: "publish a plugin from wago-plugin.json",
			Flags: []Flag{
				{Name: "manifest", Arg: "<p>", Help: "manifest path (default wago-plugin.json)"},
				{Name: "version", Arg: "<v>", Help: "version to publish (default: newest git tag)"},
				{Name: "commit", Arg: "<c>", Help: "commit SHA (default: git HEAD)"},
				{Name: "notes", Arg: "<s>", Help: "release notes"},
				{Name: "category", Arg: "<c>", Help: "package category"},
				{Name: "tags", Arg: "<a,b>", Help: "comma-separated tags"},
			},
			Run: registryPublish,
		},
		{
			Name: "unpublish", Group: "registry",
			Summary: "remove a package or one version",
			Args:    "<name>[@version]",
			Flags:   []Flag{{Name: "yes", Bool: true, Help: "skip the confirmation prompt"}},
			Run:     registryUnpublish,
		},
		{
			Name: "deprecate", Group: "registry",
			Summary: "deprecate a package/version",
			Args:    "<name>[@version]",
			Flags: []Flag{
				{Name: "message", Arg: "<m>", Help: "deprecation notice"},
				{Name: "undo", Bool: true, Help: "reverse a deprecation"},
			},
			Run: registryDeprecate,
		},
	}
}
