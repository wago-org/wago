//go:build !windows

package wagocli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runSelector drives the multi-select in raw terminal mode, repainting on each
// keypress, and returns whether the user cancelled (esc). When stdin isn't an
// interactive terminal, or raw mode can't be entered, it makes no changes and
// returns false so the caller keeps the pre-seeded selection.
//
// Raw mode is toggled with stty rather than a termios cgo/x-sys dependency, so
// the CLI stays dependency-free; `stty -g` captures the exact prior settings so
// they're restored precisely on exit.
func runSelector(m *multiSelect) (cancelled bool) {
	if !stdinIsTTY() {
		return false
	}
	restore, err := makeRaw()
	if err != nil {
		return false
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
	paint()

	buf := make([]byte, 8)
	for {
		n, err := in.Read(buf)
		if err != nil {
			return true // read failure: treat like cancel
		}
		done, cancel := m.apply(decodeKey(buf[:n]))
		paint()
		if done {
			return cancel
		}
	}
}

// stdinIsTTY reports whether standard input is an interactive terminal.
func stdinIsTTY() bool {
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
