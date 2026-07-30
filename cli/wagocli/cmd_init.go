package wagocli

import "fmt"

func initCommand() *Cmd {
	return &Cmd{
		Name:    "init",
		Summary: "initialize a local Wago project",
		Run: func(*Ctx) {
			created, err := initializeProject(".")
			if err != nil {
				fatal("init: %v", err)
			}
			ensureGitignore(".wago/")
			if created {
				fmt.Println(cyan("Initialized wago.json"))
				return
			}
			fmt.Println(dim("wago.json already initialized"))
		},
	}
}
