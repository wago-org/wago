//go:build !windows

package tui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runSelector drives the selector in raw terminal mode, repainting on each
// keypress. submitted is false when no interactive terminal is available;
// cancelled reports an explicit escape or input failure.
//
// Raw mode is toggled with stty rather than a termios cgo/x-sys dependency, so
// the CLI stays dependency-free; `stty -g` captures the exact prior settings so
// they're restored precisely on exit.
func Run(m selectorModel) (submitted, cancelled bool) {
	if !StdinIsTTY() {
		return false, false
	}
	restore, err := makeRaw()
	if err != nil {
		return false, false
	}
	defer restore()

	in := bufio.NewReader(os.Stdin)
	prev := 0
	paint := func() {
		if prev > 0 {
			fmt.Fprintf(os.Stderr, "\x1b[%dA\x1b[J", prev) // up over the old frame, clear down
		}
		f := m.frame()
		fmt.Fprint(os.Stderr, strings.ReplaceAll(f, "\n", "\r\n"))
		prev = strings.Count(f, "\n")
	}
	clear := func() {
		if prev > 0 {
			fmt.Fprintf(os.Stderr, "\x1b[%dA\x1b[J", prev)
			prev = 0
		}
	}
	paint()

	buf := make([]byte, 8)
	for {
		n, err := in.Read(buf)
		if err != nil {
			clear()
			return false, true // read failure: treat like cancel
		}
		done, cancel := m.apply(decodeKey(buf[:n]))
		paint()
		if done {
			clear()
			return !cancel, cancel
		}
	}
}

// stdinIsTTY reports whether standard input is an interactive terminal.
func StdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// makeRaw switches the terminal to raw, no-echo mode and returns a restore func
// that reinstates the captured settings.
func makeRaw() (func(), error) {
	saved, err := sttyOutput("-g")
	if err != nil {
		return nil, err
	}
	if err := stty("raw", "-echo"); err != nil {
		return nil, err
	}
	return func() {
		_ = stty(strings.Fields(strings.TrimSpace(saved))...)
		fmt.Fprint(os.Stderr, "\r\n")
	}, nil
}

func stty(args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func sttyOutput(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	return string(out), err
}
