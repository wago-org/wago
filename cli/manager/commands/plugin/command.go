// Package plugin defines shared pieces of the wago plugin command tree.
package plugin

import "github.com/wago-org/wago/cli/internal/command"

func Command(summary string, children []*command.Cmd) *command.Cmd {
	return &command.Cmd{
		Name: "plugin", Aliases: []string{"plugins"},
		Summary: summary, Children: children,
	}
}

func GlobalFlag() command.Flag {
	return command.Flag{Name: "global", Short: "g", Bool: true, Help: "use the shared user-wide plugins"}
}

func LocalFlag() command.Flag {
	return command.Flag{Name: "local", Short: "l", Bool: true, Help: "use this project's plugins"}
}
