package command

import (
	"fmt"
	"strings"
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
	lookup := flagLookup(c.Flags)
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
	return ctx, nil
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
