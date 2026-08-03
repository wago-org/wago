package command

import (
	"fmt"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
)

// WantsHelp reports whether help appears before positional pass-through begins.
func WantsHelp(args []string, passThrough bool, flags []Flag) bool {
	lookup := flagLookup(flags)
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			return false
		}
		if arg == "-h" || arg == "--help" {
			return true
		}
		if passThrough && (arg == "" || arg[0] != '-') {
			return false
		}
		name, inline := splitFlag(arg)
		if flag := lookup[name]; flag != nil && !flag.Bool && !inline && index+1 < len(args) {
			index++
		}
	}
	return false
}

// Parse accepts long and short flags, inline values, a positional terminator,
// and first-positional pass-through for guest arguments.
func (c *Cmd) Parse(path string, args []string) (*Ctx, error) {
	ctx := &Ctx{Cmd: c, Path: path, strs: map[string]string{}, bools: map[string]bool{}}
	lookup := flagLookup(c.AllFlags())
	raw, passThrough := false, false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case raw || passThrough:
			ctx.Args = append(ctx.Args, arg)
			continue
		case arg == "--":
			raw = true
			continue
		case arg == "-" || arg == "" || arg[0] != '-':
			ctx.Args = append(ctx.Args, arg)
			passThrough = c.PassThrough
			continue
		}
		name, inlineValue, inline := splitFlagValue(arg)
		flag := lookup[name]
		if flag == nil {
			return nil, fmt.Errorf("unknown flag %s", name)
		}
		if flag.Bool {
			if inline {
				return nil, fmt.Errorf("flag --%s takes no value", flag.Name)
			}
			ctx.bools[flag.Name] = true
			continue
		}
		switch {
		case inline:
			ctx.strs[flag.Name] = inlineValue
		case index+1 < len(args):
			ctx.strs[flag.Name] = args[index+1]
			index++
		default:
			return nil, fmt.Errorf("flag --%s needs a value", flag.Name)
		}
	}
	options := automation.Options{
		JSON: ctx.Bool("json"), NoInput: ctx.Bool("no-input"), DryRun: ctx.Bool("dry-run"),
		Locked: ctx.Bool("locked"), Offline: ctx.Bool("offline"),
	}
	automation.Configure(automation.Merge(options))
	if automation.JSON() && !c.Supports(JSONOutput) && !(automation.DryRun() && c.Supports(DryRun)) {
		return nil, fmt.Errorf("--json is not supported by %s", c.Label(path))
	}
	if automation.DryRun() && !c.Supports(DryRun) {
		return nil, fmt.Errorf("--dry-run is not supported by %s", c.Label(path))
	}
	return ctx, nil
}

// ConfigureAutomation records automation flags from one command invocation
// without executing it. Managers use this before handing runtime-owned commands
// to another executable so pre-handoff errors honor --json and --no-input too.
// For pass-through commands, flags after the first positional belong to the
// guest and are deliberately ignored.
func ConfigureAutomation(c *Cmd, args []string) {
	options := automation.Merge(automation.FromEnv())
	lookup := flagLookup(c.AllFlags())
	raw, passThrough := false, false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case raw || passThrough:
			continue
		case arg == "--":
			raw = true
			continue
		case arg == "-" || arg == "" || arg[0] != '-':
			passThrough = c.PassThrough
			continue
		}
		name, _, inline := splitFlagValue(arg)
		flag := lookup[name]
		if flag == nil {
			continue
		}
		switch flag.Name {
		case "json":
			options.JSON = true
		case "no-input":
			options.NoInput = true
		case "dry-run":
			options.DryRun = true
		case "locked":
			options.Locked = true
		case "offline":
			options.Offline = true
		}
		if !flag.Bool && !inline && index+1 < len(args) {
			index++
		}
	}
	automation.Configure(options)
}

func (c *Cmd) AllFlags() []Flag {
	flags := append([]Flag(nil), c.Flags...)
	flags = append(flags, c.automationFlags()...)
	return append(flags, c.Knobs...)
}

func (c *Cmd) automationFlags() []Flag {
	var flags []Flag
	if c.Supports(JSONOutput) {
		flags = append(flags, Flag{Name: "json", Short: "j", Bool: true, Help: "emit machine-readable JSON"})
	} else if c.Supports(DryRun) {
		flags = append(flags, Flag{Name: "json", Short: "j", Bool: true, Help: "emit the --dry-run plan as JSON"})
	}
	if c.Supports(DryRun) {
		flags = append(flags, Flag{Name: "dry-run", Bool: true, Help: "show the plan without changing anything"})
	}
	return append(flags,
		Flag{Name: "no-input", Bool: true, Help: "never prompt; fail when required input is missing"},
		Flag{Name: "locked", Bool: true, Help: "fail rather than change wago-lock.json or wago.json"},
		Flag{Name: "offline", Bool: true, Help: "use only installed and cached resources"},
	)
}

func flagLookup(flags []Flag) map[string]*Flag {
	lookup := make(map[string]*Flag, len(flags)*2)
	for index := range flags {
		flag := &flags[index]
		lookup["--"+flag.Name] = flag
		if flag.Short != "" {
			lookup["-"+flag.Short] = flag
		}
	}
	return lookup
}

func splitFlag(arg string) (name string, inline bool) {
	if index := strings.IndexByte(arg, '='); index >= 0 {
		return arg[:index], true
	}
	return arg, false
}

func splitFlagValue(arg string) (name, value string, inline bool) {
	if index := strings.IndexByte(arg, '='); index >= 0 {
		return arg[:index], arg[index+1:], true
	}
	return arg, "", false
}
