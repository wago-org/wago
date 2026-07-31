package options

import "github.com/wago-org/wago/cli/internal/command"

type Selection struct {
	Downloads bool
	Builds    bool
}

func Flags() []command.Flag {
	return []command.Flag{
		{Name: "downloads", Short: "d", Bool: true, Help: "include downloaded and compiled module cache"},
		{Name: "builds", Short: "b", Bool: true, Help: "include local and global plugin builds"},
		{Name: "all", Short: "a", Bool: true, Help: "include every regenerable cache"},
	}
}

func Selected(ctx *command.Ctx) Selection {
	all := ctx.Bool("all")
	return Selection{Downloads: all || ctx.Bool("downloads"), Builds: all || ctx.Bool("builds")}
}
