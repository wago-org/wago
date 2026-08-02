package command

import (
	"fmt"
	"io"
	"strings"

	"github.com/wago-org/wago/cli/internal/ui"
)

// InvocationWantsHelp reports whether args request help for cmd or one of its
// descendants without crossing a positional pass-through seam.
func InvocationWantsHelp(cmd *Cmd, args []string) bool {
	if len(cmd.Children) == 0 {
		normalized := args
		if cmd.Normalize != nil {
			var err error
			normalized, err = cmd.Normalize(args)
			if err != nil {
				return false
			}
		}
		return WantsHelp(normalized, cmd.PassThrough, cmd.Flags)
	}
	if WantsHelp(args, true, cmd.AllFlags()) {
		return true
	}
	if len(args) == 0 {
		return cmd.Run == nil
	}
	child := cmd.Child(args[0])
	return child != nil && InvocationWantsHelp(child, args[1:])
}

func (c *Cmd) PrintHelp(output io.Writer, path string) {
	var text strings.Builder
	fmt.Fprintf(&text, "%s %s", ui.Bold("Usage:"), path)
	if len(c.Children) > 0 {
		command := "<command>"
		if c.Run != nil {
			command = "[command]"
		}
		fmt.Fprintf(&text, " %s", ui.Dim(command))
	}
	if c.Args != "" {
		fmt.Fprintf(&text, " %s", ui.Dim(c.Args))
	}
	if len(c.AllFlags()) > 0 {
		fmt.Fprintf(&text, " %s", ui.Dim("[flags]"))
	}
	text.WriteByte('\n')
	if c.Summary != "" {
		fmt.Fprintf(&text, "\n%s\n", c.Summary)
	}
	if len(c.Children) > 0 {
		writeChildren(&text, c.Children)
	}
	writeFlags(&text, c.displayFlags())
	if c.Long != "" {
		fmt.Fprintf(&text, "\n%s\n", strings.TrimRight(c.Long, "\n"))
	}
	fmt.Fprint(output, text.String())
}

func writeChildren(text *strings.Builder, children []*Cmd) {
	fmt.Fprintf(text, "\n%s\n", ui.Bold("Commands:"))
	nameWidth, argWidth := 0, 0
	for _, child := range children {
		nameWidth = max(nameWidth, len(child.Name))
		argWidth = max(argWidth, len(ArgSynopsis(child)))
	}
	for _, child := range children {
		args := fmt.Sprintf("%-*s", argWidth, ArgSynopsis(child))
		fmt.Fprintf(text, "  %-*s  %s  %s\n", nameWidth, child.Name, ui.Dim(args), child.Summary)
	}
}

func writeFlags(text *strings.Builder, flags []Flag) {
	labels := make([]string, 0, len(flags))
	descriptions := make([]string, 0, len(flags))
	for index := 0; index < len(flags); index++ {
		flag := flags[index]
		if flag.Bool && index+1 < len(flags) && flags[index+1].Name == "no-"+flag.Name && flags[index+1].Bool {
			label := "--<no->" + flag.Name
			if flag.Short != "" {
				label += ", -" + flag.Short
			}
			labels = append(labels, label)
			descriptions = append(descriptions, flag.Help)
			index++
			continue
		}
		labels = append(labels, flagLabel(flag))
		descriptions = append(descriptions, flag.Help)
	}
	width := 0
	for _, label := range labels {
		width = max(width, len(label))
	}
	fmt.Fprintf(text, "\n%s\n", ui.Bold("Flags:"))
	for index, label := range labels {
		label = fmt.Sprintf("%-*s", width, label)
		fmt.Fprintf(text, "  %s  %s\n", DimHelpSyntax(label, ui.Dim), descriptions[index])
	}
}

func (c *Cmd) displayFlags() []Flag {
	flags := append([]Flag(nil), c.Flags...)
	flags = append(flags, c.automationFlags()...)
	flags = append(flags, Flag{Name: "help", Short: "h", Bool: true, Help: "show this help"})
	return append(flags, c.Knobs...)
}

func DimHelpSyntax(value string, style func(string) string) string {
	var text strings.Builder
	for len(value) > 0 {
		start := strings.IndexAny(value, "<[")
		if start < 0 {
			text.WriteString(value)
			break
		}
		text.WriteString(value[:start])
		close := byte('>')
		if value[start] == '[' {
			close = ']'
		}
		end := strings.IndexByte(value[start+1:], close)
		if end < 0 {
			text.WriteString(value[start:])
			break
		}
		end += start + 1
		text.WriteString(style(value[start : end+1]))
		value = value[end+1:]
	}
	return text.String()
}

func ArgSynopsis(command *Cmd) string {
	if len(command.Children) > 0 {
		if command.Run != nil {
			return "[command]"
		}
		return "<command>"
	}
	return command.Args
}

func flagLabel(flag Flag) string {
	label := "--" + flag.Name
	if flag.Short != "" {
		label += ", -" + flag.Short
	}
	if !flag.Bool && flag.Arg != "" {
		label += " " + flag.Arg
	}
	return label
}
