package dir

import "github.com/wago-org/wago/cli/internal/command"

type Environment interface {
	CacheDir()
}

func Command(environment Environment) *command.Cmd {
	return &command.Cmd{
		Name:    "dir",
		Summary: "print the global cache directory",
		Run:     func(*command.Ctx) { environment.CacheDir() },
	}
}
