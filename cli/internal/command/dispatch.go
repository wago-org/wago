package command

import (
	"fmt"
	"os"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/ui"
)

// Dispatch resolves args against the command tree and invokes one leaf.
func (c *Cmd) Dispatch(path string, args []string) {
	if len(c.Children) > 0 {
		var err error
		args, err = automation.ParseLeading(args)
		if err != nil {
			ui.Usage("%s: %v", c.Label(path), err)
		}
		if wantsLeadingHelp(args, c.Flags) {
			c.PrintHelp(os.Stdout, path)
			return
		}
		if c.Run != nil && (len(args) == 0 || args[0] == "" || args[0][0] == '-') {
			ctx, err := c.Parse(path, args)
			if err != nil {
				ui.Usage("%s: %v", c.Label(path), err)
			}
			c.Run(ctx)
			return
		}
		if len(args) == 0 {
			c.PrintHelp(os.Stdout, path)
			return
		}
		if child := c.Child(args[0]); child != nil {
			child.Dispatch(path+" "+child.Name, args[1:])
			return
		}
		if automation.JSON() {
			hint := path + " --help"
			if suggestion := SuggestChild(c, args[0]); suggestion != "" {
				hint = path + " " + suggestion + " --help"
			}
			ui.UsageHint(hint, "%s: unknown subcommand %q", c.Label(path), args[0])
		}
		fmt.Fprintf(os.Stderr, "%s %s: unknown subcommand %q\n\n", ui.Red("wago:"), c.Label(path), args[0])
		if suggestion := SuggestChild(c, args[0]); suggestion != "" {
			fmt.Fprintf(os.Stderr, "Did you mean %q?\n\n", suggestion)
		}
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
	if len(c.Knobs) != 0 && WantsOptimizationHelp(args, c.PassThrough, c.AllFlags()) {
		c.PrintOptimizationHelp(os.Stdout, path)
		return
	}
	if WantsHelp(args, c.PassThrough, c.AllFlags()) {
		c.PrintHelp(os.Stdout, path)
		return
	}
	ctx, err := c.Parse(path, args)
	if err != nil {
		ui.Usage("%s: %v", c.Label(path), err)
	}
	c.Run(ctx)
}
