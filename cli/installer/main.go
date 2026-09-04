package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago/internal/managedrelease"
)

type key struct {
	name string
	rune rune
}

type style struct {
	cyan  string
	red   string
	dim   string
	bold  string
	reset string
}

type radioItem struct {
	label  string
	desc   string
	value  string
	status string
}

var version = "dev"

func main() {
	executable, err := managedrelease.ExecutablePath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if name := filepath.Base(executable); name == "wago" || (runtime.GOOS == "windows" && strings.EqualFold(name, "wago.exe")) {
		dispatched, err := managedrelease.Dispatch()
		if !dispatched && err == nil {
			err = fmt.Errorf("wago release selection is missing; reinstall Wago")
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(managedrelease.ExitCode(err))
	}
	if len(os.Args) == 1 || (len(os.Args) == 2 && os.Args[1] == "install") {
		if err := runInstaller(); err != nil {
			s := colors()
			fmt.Fprintf(os.Stderr, "\n%sWago could not be installed:%s %v\n", s.red, s.reset, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println(version)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "verify" {
		if len(os.Args) != 4 {
			os.Exit(2)
		}
		seconds, err := strconv.Atoi(os.Args[3])
		if err != nil || seconds < 1 {
			os.Exit(2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
		defer cancel()
		err = exec.CommandContext(ctx, os.Args[2], "self", "--help").Run()
		if ctx.Err() != nil || err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "spinner" {
		if len(os.Args) != 4 {
			os.Exit(2)
		}
		spinner(os.Args[2], os.Args[3])
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "status" {
		if len(os.Args) != 4 {
			os.Exit(2)
		}
		status(os.Args[2], os.Args[3])
		return
	}
	if len(os.Args) != 3 {
		os.Exit(2)
	}
	mode, output := os.Args[1], os.Args[2]
	var value string
	var ok bool
	switch mode {
	case "install-dir":
		value, ok = installDir()
	case "radio":
		value, ok = environmentRadio()
	case "reinstall":
		value, ok = radio("Reinstall method", []radioItem{
			{"Full", "Reset everything, including plugins and settings", "full", ""},
			{"Partial", "Reset Wago but keep global plugins for reinstall", "partial", ""},
			{"Minimal", "Replace binaries and keep existing state", "minimal", ""},
		}, 2)
	case "path":
		home := firstEnv("HOME", "USERPROFILE")
		value, ok = radio(pathSetupQuestion(), pathSetupItems(pathTargets(home)), 0)
	default:
		os.Exit(2)
	}
	if !ok {
		os.Exit(1)
	}
	if err := writeSelection(output, value); err != nil {
		os.Exit(2)
	}
}

func writeSelection(output, value string) error {
	return os.WriteFile(output, []byte(value+"\n"), 0o600)
}

func environmentRadio() (string, bool) {
	title := os.Getenv("WAGO_UI_RADIO_TITLE")
	lines := strings.Split(os.Getenv("WAGO_UI_RADIO_ITEMS"), "\n")
	items := make([]radioItem, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "|", 4)
		if len(fields) < 3 {
			return "", false
		}
		item := radioItem{label: fields[0], desc: fields[1], value: fields[2]}
		if len(fields) == 4 {
			item.status = fields[3]
		}
		items = append(items, item)
	}
	if title == "" || len(items) == 0 {
		return "", false
	}
	cursor := 0
	if raw := os.Getenv("WAGO_UI_RADIO_CURSOR"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed >= len(items) {
			return "", false
		}
		cursor = parsed
	}
	return radio(title, items, cursor)
}

func spinner(stop, label string) {
	if !stderrIsConsole() {
		return
	}
	enableVirtualTerminal()
	running := stop + ".running"
	if err := os.WriteFile(running, nil, 0o600); err != nil {
		return
	}
	defer os.Remove(running)
	s := colors()
	for index := 0; ; index++ {
		if _, err := os.Stat(stop); err == nil {
			clearProgressLine()
			return
		}
		clearProgressLine()
		fmt.Fprintf(os.Stderr, "%s%s%s %s", s.dim, spinnerFrames[index%len(spinnerFrames)], s.reset, label)
		time.Sleep(80 * time.Millisecond)
	}
}

func status(kind, label string) {
	if stderrIsConsole() {
		enableVirtualTerminal()
		clearProgressLine()
	}
	s := colors()
	switch kind {
	case "done", "finish":
		fmt.Fprintf(os.Stderr, "%s\u2713%s %s\n", s.cyan, s.reset, label)
	case "fail":
		fmt.Fprintf(os.Stderr, "%s\u2717%s %s\n", s.red, s.reset, label)
	case "retry":
		fmt.Fprintf(os.Stderr, "%s\u2192%s %s\n", s.dim, s.reset, label)
	}
}

func colors() style {
	return colorsFor(stderrIsConsole())
}

func colorsFor(isConsole bool) style {
	if !isConsole || os.Getenv("NO_COLOR") != "" {
		return style{}
	}
	return style{cyan: "\x1b[36m", red: "\x1b[31m", dim: "\x1b[2m", bold: "\x1b[1m", reset: "\x1b[0m"}
}

var spinnerFrames = []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}

func radio(title string, items []radioItem, cursor int) (string, bool) {
	c, interactive := openConsole()
	if !interactive {
		if title == pathSetupQuestion() {
			return "", false
		}
		return items[cursor].value, true
	}
	defer c.close()
	s := colors()
	width := 0
	for _, item := range items {
		if len(item.label) > width {
			width = len(item.label)
		}
	}
	painted := false
	render := func() {
		if painted {
			clearConsoleLines(len(items) + 2)
		}
		fmt.Fprintf(os.Stderr, "%s%s%s\n", s.bold, title, s.reset)
		for index, item := range items {
			lead, mark, labelStyle := "  ", "\u25cb", ""
			if index == cursor {
				lead, mark = s.cyan+"\u203a "+s.reset, s.cyan+"\u25c9"+s.reset
			}
			if item.status != "" {
				labelStyle = s.bold
			}
			fmt.Fprintf(os.Stderr, "%s%s %s%-*s%s", lead, mark, labelStyle, width, item.label, s.reset)
			if item.desc != "" {
				fmt.Fprintf(os.Stderr, "  %s%s%s", s.dim, item.desc, s.reset)
			}
			if item.status != "" {
				fmt.Fprintf(os.Stderr, "  %s(%s)%s", s.dim, item.status, s.reset)
			}
			fmt.Fprintln(os.Stderr)
		}
		fmt.Fprintf(os.Stderr, "%s\u2191/\u2193 move \u00b7 enter/\u2192 select \u00b7 esc cancel%s\n", s.dim, s.reset)
		painted = true
	}
	render()
	for {
		pressed := c.readKey()
		switch pressed.name {
		case "up":
			if cursor > 0 {
				cursor--
			}
		case "down":
			if cursor+1 < len(items) {
				cursor++
			}
		case "enter", "right":
			clearConsoleLines(len(items) + 2)
			return items[cursor].value, true
		case "escape":
			clearConsoleLines(len(items) + 2)
			return "", false
		default:
			if pressed.rune == 'k' || pressed.rune == 'K' {
				if cursor > 0 {
					cursor--
				}
			} else if pressed.rune == 'j' || pressed.rune == 'J' {
				if cursor+1 < len(items) {
					cursor++
				}
			} else if pressed.rune == 'q' || pressed.rune == 'Q' {
				clearConsoleLines(len(items) + 2)
				return "", false
			} else {
				continue
			}
		}
		render()
	}
}

func installDir() (string, bool) {
	defaultDir, cwd, home := os.Getenv("WAGO_UI_BIN_DIR"), os.Getenv("WAGO_UI_CWD"), os.Getenv("HOME")
	if runtime.GOOS == "windows" || home == "" {
		home = os.Getenv("USERPROFILE")
	}
	c, interactive := openConsole()
	if !interactive {
		return defaultDir, true
	}
	defer c.close()
	s := colors()
	focus, suggestionCursor := 0, 0
	input := ""
	paintedLines := 0
	for {
		suggestions := pathSuggestions(input, cwd, home)
		if len(suggestions) == 0 {
			suggestionCursor = -1
		} else if suggestionCursor < 0 || suggestionCursor >= len(suggestions) {
			suggestionCursor = 0
		}
		if paintedLines > 0 {
			clearConsoleLines(paintedLines)
		}
		fmt.Fprintf(os.Stderr, "%sWhere should Wago be installed?%s\n", s.bold, s.reset)
		if focus == 0 {
			fmt.Fprintf(os.Stderr, "%s\u203a %s\u25c9%s %s\n", s.cyan, s.cyan, s.reset, displayPath(defaultDir, home))
		} else {
			fmt.Fprintf(os.Stderr, "  \u25cb %s\n", displayPath(defaultDir, home))
		}
		if focus == 1 {
			shown := input
			if shown == "" {
				shown = s.dim + "Type a directory" + s.reset
			}
			fmt.Fprintf(os.Stderr, "%s\u203a %s\u25c9%s %s\n", s.cyan, s.cyan, s.reset, shown)
		} else {
			fmt.Fprintln(os.Stderr, "  \u25cb Custom")
		}
		for index, suggestion := range suggestions {
			if index == suggestionCursor {
				fmt.Fprintf(os.Stderr, "    %s\u203a %s%s\n", s.cyan, suggestion, s.reset)
			} else {
				fmt.Fprintf(os.Stderr, "      %s%s%s\n", s.dim, suggestion, s.reset)
			}
		}
		if focus == 0 {
			fmt.Fprintf(os.Stderr, "%s\u2191/\u2193 move \u00b7 enter/\u2192 select \u00b7 esc cancel%s\n", s.dim, s.reset)
		} else {
			fmt.Fprintf(os.Stderr, "%s\u2191/\u2193 suggestions \u00b7 type path \u00b7 tab/\u2192 complete \u00b7 \u2190 parent \u00b7 enter select \u00b7 esc cancel%s\n", s.dim, s.reset)
		}
		paintedLines = len(suggestions) + 4

		pressed := c.readKey()
		switch pressed.name {
		case "escape":
			clearConsoleLines(paintedLines)
			return "", false
		case "enter":
			if focus == 0 {
				clearConsoleLines(paintedLines)
				return defaultDir, true
			}
			if input != "" {
				clearConsoleLines(paintedLines)
				return resolvePath(input, cwd, home), true
			}
		case "up":
			if focus == 1 && suggestionCursor > 0 {
				suggestionCursor--
			} else if focus == 1 {
				focus = 0
			}
		case "down":
			if focus == 0 {
				focus = 1
			} else if suggestionCursor+1 < len(suggestions) {
				suggestionCursor++
			}
		case "right", "tab":
			if focus == 0 && pressed.name == "right" {
				clearConsoleLines(paintedLines)
				return defaultDir, true
			}
			if focus == 1 && suggestionCursor >= 0 {
				input = suggestions[suggestionCursor]
				suggestionCursor = 0
			}
		case "left":
			if focus == 1 {
				input, focus = parentInput(input, focus)
				suggestionCursor = 0
			}
		case "backspace":
			if focus == 1 && input != "" {
				runes := []rune(input)
				input = string(runes[:len(runes)-1])
				suggestionCursor = 0
			}
		default:
			if focus == 1 && pressed.rune >= 32 {
				input += string(pressed.rune)
				suggestionCursor = 0
			}
		}
	}
}

func pathSuggestions(input, cwd, home string) []string {
	if input == "" {
		return nil
	}
	normalized := normalizeInput(input)
	separator := string(os.PathSeparator)
	parentText, prefix := "", ""
	if normalized == "~" {
		parentText = "~"
	} else if strings.HasSuffix(normalized, separator) {
		parentText = strings.TrimSuffix(normalized, separator)
	} else if index := strings.LastIndex(normalized, separator); index >= 0 {
		parentText, prefix = normalized[:index], normalized[index+1:]
	} else {
		parentText, prefix = cwd, normalized
	}
	parent := resolvePath(parentText, cwd, home)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var result []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(strings.ToLower(entry.Name()), strings.ToLower(prefix)) {
			result = append(result, displayPath(filepath.Join(parent, entry.Name()), home)+separator)
		}
	}
	sort.Strings(result)
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}

func resolvePath(input, cwd, home string) string {
	input = normalizeInput(input)
	separator := string(os.PathSeparator)
	if input == "~" {
		return home
	}
	if strings.HasPrefix(input, "~"+separator) {
		return filepath.Clean(filepath.Join(home, input[2:]))
	}
	if filepath.IsAbs(input) {
		return filepath.Clean(input)
	}
	return filepath.Clean(filepath.Join(cwd, input))
}

func displayPath(path, home string) string {
	path = filepath.Clean(path)
	if strings.EqualFold(path, home) {
		return "~"
	}
	separator := string(os.PathSeparator)
	prefix := filepath.Clean(home) + separator
	if strings.HasPrefix(strings.ToLower(path), strings.ToLower(prefix)) {
		return "~" + separator + path[len(prefix):]
	}
	return path
}

func parentInput(input string, focus int) (string, int) {
	separator := string(os.PathSeparator)
	input = strings.TrimSuffix(normalizeInput(input), separator)
	if input == "" {
		return "", 0
	}
	if input == "~" {
		return "~" + separator, focus
	}
	parent := filepath.Dir(input)
	if parent == "." {
		return "", focus
	}
	if parent == "~" {
		return "~" + separator, focus
	}
	if filepath.VolumeName(parent)+separator == parent {
		return parent, focus
	}
	return parent + separator, focus
}

func normalizeInput(input string) string {
	if os.PathSeparator == '\\' {
		return strings.ReplaceAll(input, "/", `\`)
	}
	return input
}
