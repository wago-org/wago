//go:build wago_profile

// Package profiling attaches the optional diagnostic command surface.
package profiling

import (
	"flag"
	"fmt"
	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/ui"
	"github.com/wago-org/wago/internal/profilecmd"
)

func Append(root *command.Cmd) *command.Cmd {
	group := &command.Cmd{Name: "profile", Summary: "record workloads and inspect native CPU profiles"}
	for _, spec := range []struct{ name, summary, args string }{
		{"record", "record a validated workload", ""},
		{"top", "show sampled hot functions and compiler statistics", "<capture>"},
		{"annotate", "join instruction hotness to compiler output", "<capture>"},
		{"diff", "compare cost per equivalent completed workload", "<baseline> <candidate>"},
		{"timeline", "show elapsed invocation and host-boundary spans", "<capture>"},
	} {
		name := spec.name
		cmd := &command.Cmd{Name: name, Summary: spec.summary, Args: spec.args}
		profilecmd.FlagSet(name).VisitAll(func(f *flag.Flag) {
			if f.Name == "control" || f.Name == "ack" || f.Name == "jit-dir" || f.Name == "supervised" {
				return
			}
			if f.Name == "json" {
				cmd.Automation |= command.JSONOutput
				return
			}
			desc := command.Flag{Name: f.Name, Help: f.Usage}
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
				desc.Bool = true
			} else {
				desc.Arg = "<value>"
			}
			if f.DefValue != "" {
				desc.Help += fmt.Sprintf(" (default: %s)", f.DefValue)
			}
			cmd.Flags = append(cmd.Flags, desc)
		})
		cmd.Run = func(ctx *command.Ctx) {
			// Go's flag parser stops at positionals. Reconstruct normalized flags from
			// the command context so the CLI also accepts flags after capture paths.
			var args []string
			for _, f := range ctx.Cmd.Flags {
				if f.Bool {
					if ctx.Bool(f.Name) {
						args = append(args, "--"+f.Name)
					}
				} else {
					for _, value := range ctx.Strings(f.Name) {
						args = append(args, "--"+f.Name+"="+value)
					}
				}
			}
			if automation.JSON() {
				args = append(args, "--json")
			}
			args = append(args, ctx.Args...)
			if err := profilecmd.Run(append([]string{name}, args...), []string{"profile"}); err != nil {
				ui.Fatal("profile %s: %v", name, err)
			}
		}
		group.Children = append(group.Children, cmd)
	}
	root.Children = append(root.Children, group)
	return root
}

// Capture handles collector re-execution without adding a public subcommand.
func Capture(args []string) bool {
	if len(args) < 2 || args[0] != "profile" || args[1] != "capture" {
		return false
	}
	if err := profilecmd.Run(args[1:], []string{"profile"}); err != nil {
		ui.Fatal("profile capture: %v", err)
	}
	return true
}

// IsInvocation lets the manager restore the profiler's normal interrupt behavior;
// its management cancellation context is not consumed by collector subprocesses.
func IsInvocation(args []string) bool { return len(args) > 0 && args[0] == "profile" }
