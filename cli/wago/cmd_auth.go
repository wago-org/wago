package main

// authCommand is the `wago auth` group: authenticate to the wago registry at
// pkg.wago.sh. The Run bodies are build-tagged (registry_net.go for the full
// build, registry_stub.go for the lean/TinyGo build); this declaration is
// unconditional so `wago auth login --help` works in every build.
func authCommand() *Cmd {
	return &Cmd{
		Name:    "auth",
		Summary: "authenticate to the registry (pkg.wago.sh)",
		Children: []*Cmd{
			{
				Name:    "login",
				Summary: "log in to the registry",
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
				Name:    "logout",
				Summary: "remove stored registry credentials",
				Run:     registryLogout,
			},
			{
				Name:    "whoami",
				Summary: "print the logged-in account",
				Run:     registryWhoami,
			},
		},
	}
}
