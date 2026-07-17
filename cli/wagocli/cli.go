package wagocli

// A tiny, zero-dependency command framework. Every wago command is a Cmd
// declared in a cmd_*.go file and hung off root by buildRoot(). The framework
// gives every command uniform flag parsing and an automatic `-h`/`--help`, so no
// command hand-rolls its own argument handling (the old CLI did, inconsistently).

import (
	"fmt"
	"os"
	"strings"
)

// Flag declares one option a command accepts.
type Flag struct {
	Name  string // canonical long name without dashes, e.g. "invoke"
	Short string // optional one-letter alias without a dash, e.g. "e"; "" if none
	Bool  bool   // presence-only (--json) vs value-taking (--invoke <name>)
	Arg   string // value placeholder shown in help, e.g. "<name>"; unused when Bool
	Help  string // one-line description for per-command help
}

// Cmd is a command or a group of subcommands. A group has Children (and no Run);
// a leaf has a Run. buildRoot() wires the whole tree.
type Cmd struct {
	Name        string
	Aliases     []string                         // alternate names, e.g. {"ls"} for list
	Summary     string                           // one line for the parent's command list
	Args        string                           // positional synopsis for help, e.g. "<file> [args...]"
	Long        string                           // optional extra prose appended to per-command help
	Flags       []Flag                           // options this leaf accepts
	PassThrough bool                             // run: stop flag parsing at the first positional (guest argv)
	Normalize   func([]string) ([]string, error) // optional pre-parse argument normalization
	Run         func(*Ctx)                       // leaf action; nil for a pure group
	Children    []*Cmd                           // subcommands; non-empty makes this a group
}

// Ctx is a parsed invocation handed to a leaf's Run.
type Ctx struct {
	Cmd   *Cmd
	Path  string   // e.g. "wago plugin inspect", for messages
	Args  []string // positionals after flag parsing (guest argv for run)
	strs  map[string]string
	bools map[string]bool
}

// Str returns the value of a value-flag (empty if unset).
func (c *Ctx) Str(name string) string { return c.strs[name] }

// Bool reports whether a boolean flag was present.
func (c *Ctx) Bool(name string) bool { return c.bools[name] }

// one returns the sole positional argument, or fatals with a usage hint naming
// what was expected (e.g. "<name>").
func (c *Ctx) one(what string) string {
	if len(c.Args) != 1 {
		fatal("%s: need exactly one %s", strings.TrimPrefix(c.Path, "wago "), what)
	}
	return c.Args[0]
}

// opt returns an optional sole positional (empty when none), fataling if more
// than one was given so `what` still documents the single-arg shape.
func (c *Ctx) opt(what string) string {
	switch len(c.Args) {
	case 0:
		return ""
	case 1:
		return c.Args[0]
	default:
		fatal("%s: accepts at most one %s", strings.TrimPrefix(c.Path, "wago "), what)
		return ""
	}
}

// child finds a subcommand by name or alias.
func (c *Cmd) child(name string) *Cmd {
	for _, ch := range c.Children {
		if ch.Name == name {
			return ch
		}
		for _, a := range ch.Aliases {
			if a == name {
				return ch
			}
		}
	}
	return nil
}

// label is the command path without the leading "wago " (for error messages).
func (c *Cmd) label(path string) string { return strings.TrimPrefix(path, "wago ") }

// Dispatch resolves args against c and runs (or delegates to) the right command.
// It is the single entry point from main() for every command.
func (c *Cmd) Dispatch(path string, args []string) {
	// A group's own -h/--help must precede the subcommand token, so `token create
	// --help` descends to create rather than printing the group's help. That's the
	// same "stop at the first positional" rule PassThrough uses.
	if wantsHelp(args, c.PassThrough || len(c.Children) > 0) {
		c.printHelp(os.Stdout, path)
		return
	}
	if len(c.Children) > 0 {
		if len(args) == 0 {
			// A bare group invocation (e.g. `wago plugin`) shows its help — every
			// group behaves the same, so there's always a discoverable menu.
			c.printHelp(os.Stdout, path)
			return
		}
		if sub := c.child(args[0]); sub != nil {
			sub.Dispatch(path+" "+sub.Name, args[1:])
			return
		}
		fmt.Fprintf(os.Stderr, "%s %s: unknown subcommand %q\n\n", red("wago:"), c.label(path), args[0])
		c.printHelp(os.Stderr, path)
		os.Exit(2)
	}
	if c.Normalize != nil {
		var err error
		args, err = c.Normalize(args)
		if err != nil {
			fatal("%s: %v", c.label(path), err)
		}
	}
	ctx, err := c.parse(path, args)
	if err != nil {
		fatal("%s: %v", c.label(path), err)
	}
	c.Run(ctx)
}

// wantsHelp reports whether -h/--help appears among the flag tokens. For a
// PassThrough command it only scans up to the first positional (the .wasm file),
// so a guest program's own --help is not mistaken for wago's.
func wantsHelp(args []string, passThrough bool) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "--help" {
			return true
		}
		if passThrough && (a == "" || a[0] != '-') {
			return false
		}
	}
	return false
}

// parse turns args into a Ctx using the command's Flags. It accepts "--name val",
// "--name=val", short "-x val"/"-x=val", bare booleans, and a "--" positional
// terminator. Unknown flags are an error (this is what makes `--help` work instead
// of being swallowed as a filename). A PassThrough command stops consuming flags at
// the first positional, so everything from the .wasm file onward is guest argv.
func (c *Cmd) parse(path string, args []string) (*Ctx, error) {
	ctx := &Ctx{Cmd: c, Path: path, strs: map[string]string{}, bools: map[string]bool{}}
	lookup := map[string]*Flag{}
	for i := range c.Flags {
		f := &c.Flags[i]
		lookup["--"+f.Name] = f
		if f.Short != "" {
			lookup["-"+f.Short] = f
		}
	}
	raw := false         // past a "--" terminator: everything is positional
	passthrough := false // PassThrough and already saw the first positional
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case raw || passthrough:
			ctx.Args = append(ctx.Args, a)
			continue
		case a == "--":
			raw = true
			continue
		case a == "-" || a == "" || a[0] != '-':
			ctx.Args = append(ctx.Args, a)
			if c.PassThrough {
				passthrough = true
			}
			continue
		}
		name, inline, hasInline := a, "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			name, inline, hasInline = a[:eq], a[eq+1:], true
		}
		f, ok := lookup[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag %s", name)
		}
		if f.Bool {
			if hasInline {
				return nil, fmt.Errorf("flag --%s takes no value", f.Name)
			}
			ctx.bools[f.Name] = true
			continue
		}
		switch {
		case hasInline:
			ctx.strs[f.Name] = inline
		case i+1 < len(args):
			ctx.strs[f.Name] = args[i+1]
			i++
		default:
			return nil, fmt.Errorf("flag --%s needs a value", f.Name)
		}
	}
	return ctx, nil
}

// printHelp renders a command's own help in the shared house style (see usage()
// for the top level): a usage line, a one-line description, then either its
// subcommands (for a group) or its flags (for a leaf), each in an aligned table.
// -h/--help is always listed last.
func (c *Cmd) printHelp(w *os.File, path string) {
	var b strings.Builder
	line := "Usage: " + path
	if len(c.Children) > 0 {
		line += " <command>"
	}
	if c.Args != "" {
		line += " " + c.Args
	}
	if len(c.Flags) > 0 {
		line += " [flags]"
	}
	fmt.Fprintf(&b, "%s\n", bold(line))
	if c.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", c.Summary)
	}
	if len(c.Children) > 0 {
		fmt.Fprintf(&b, "\n%s\n", bold("Commands:"))
		nameW, argW := 0, 0
		for _, ch := range c.Children {
			nameW = max(nameW, len(ch.Name))
			argW = max(argW, len(cmdArg(ch)))
		}
		for _, ch := range c.Children {
			fmt.Fprintf(&b, "  %-*s  %-*s  %s\n", nameW, ch.Name, argW, cmdArg(ch), ch.Summary)
		}
	}
	// Flags, long form first, with -h/--help appended; the label column is sized
	// to the widest label so descriptions align.
	labels := make([]string, 0, len(c.Flags)+1)
	helps := make([]string, 0, len(c.Flags)+1)
	for i := 0; i < len(c.Flags); i++ {
		f := c.Flags[i]
		if f.Bool && i+1 < len(c.Flags) && c.Flags[i+1].Name == "no-"+f.Name && c.Flags[i+1].Bool {
			labels = append(labels, "--<no->"+f.Name)
			helps = append(helps, f.Help)
			i++
			continue
		}
		labels = append(labels, flagLabel(f))
		helps = append(helps, f.Help)
	}
	labels = append(labels, "--help, -h")
	helps = append(helps, "show this help")
	w0 := 0
	for _, l := range labels {
		w0 = max(w0, len(l))
	}
	fmt.Fprintf(&b, "\n%s\n", bold("Flags:"))
	for i, l := range labels {
		fmt.Fprintf(&b, "  %-*s  %s\n", w0, l, helps[i])
	}
	if c.Long != "" {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimRight(c.Long, "\n"))
	}
	fmt.Fprint(w, b.String())
}

// cmdArg is a command's positional synopsis for a command table: "<command>" for
// a group, otherwise its Args (possibly empty).
func cmdArg(c *Cmd) string {
	if len(c.Children) > 0 {
		return "<command>"
	}
	return c.Args
}

// flagLabel renders a flag's left-column help label, long form first, e.g.
// "--invoke, -e <name>" or the bare "--json".
func flagLabel(f Flag) string {
	s := "--" + f.Name
	if f.Short != "" {
		s += ", -" + f.Short
	}
	if !f.Bool && f.Arg != "" {
		s += " " + f.Arg
	}
	return s
}
