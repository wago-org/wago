package command

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/cli/internal/ui"
)

// Dispatch resolves args against the command tree and invokes one leaf.
func (c *Cmd) Dispatch(path string, args []string) {
	if len(c.Children) > 0 {
		if WantsHelp(args, true, c.Flags) || len(args) == 0 {
			c.PrintHelp(os.Stdout, path)
			return
		}
		if child := c.Child(args[0]); child != nil {
			child.Dispatch(path+" "+child.Name, args[1:])
			return
		}
		fmt.Fprintf(os.Stderr, "%s %s: unknown subcommand %q\n\n", ui.Red("wago:"), c.Label(path), args[0])
		c.PrintHelp(os.Stderr, path)
		os.Exit(2)
	}
	if c.Normalize != nil {
		var err error
		args, err = c.Normalize(args)
		if err != nil {
			ui.Fatal("%s: %v", c.Label(path), err)
		}
	}
	if WantsHelp(args, c.PassThrough, c.Flags) {
		c.PrintHelp(os.Stdout, path)
		return
	}
	ctx, err := c.Parse(path, args)
	if err != nil {
		ui.Fatal("%s: %v", c.Label(path), err)
	}
	c.Run(ctx)
}
