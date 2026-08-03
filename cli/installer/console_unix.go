//go:build !windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
)

type console struct {
	reader *bufio.Reader
	input  *os.File
	saved  string
}

func openConsole() (*console, bool) {
	input, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	info, err := input.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 || os.Getenv("TERM") == "dumb" {
		_ = input.Close()
		return nil, false
	}
	saved, err := sttyOutput(input, "-g")
	if err != nil {
		_ = input.Close()
		return nil, false
	}
	if err := stty(input, "-echo", "-icanon", "min", "1", "time", "0"); err != nil {
		_ = input.Close()
		return nil, false
	}
	return &console{reader: bufio.NewReader(input), input: input, saved: strings.TrimSpace(saved)}, true
}

func (c *console) close() {
	if c.saved != "" {
		_ = stty(c.input, strings.Fields(c.saved)...)
	}
	_ = c.input.Close()
}

func (c *console) readKey() key {
	value, err := c.reader.ReadByte()
	if err != nil {
		return key{name: "escape"}
	}
	switch value {
	case 8, 127:
		return key{name: "backspace"}
	case '\t':
		return key{name: "tab"}
	case '\r', '\n':
		return key{name: "enter"}
	case 27:
		_ = stty(c.input, "min", "0", "time", "1")
		defer stty(c.input, "min", "1", "time", "0")
		second, err := c.reader.ReadByte()
		if err != nil {
			return key{name: "escape"}
		}
		if second != '[' {
			return key{name: "escape"}
		}
		third, err := c.reader.ReadByte()
		if err != nil {
			return key{name: "escape"}
		}
		switch third {
		case 'A':
			return key{name: "up"}
		case 'B':
			return key{name: "down"}
		case 'C':
			return key{name: "right"}
		case 'D':
			return key{name: "left"}
		}
		return key{name: "escape"}
	}
	if value < 0x80 {
		return key{rune: rune(value)}
	}
	_ = c.reader.UnreadByte()
	r, _, err := c.reader.ReadRune()
	if err != nil {
		return key{}
	}
	return key{rune: r}
}

func stderrIsConsole() bool {
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb"
}

func enableVirtualTerminal() {}

func stty(input *os.File, args ...string) error {
	command := exec.Command("stty", args...)
	command.Stdin = input
	return command.Run()
}

func sttyOutput(input *os.File, args ...string) (string, error) {
	command := exec.Command("stty", args...)
	command.Stdin = input
	output, err := command.Output()
	return string(output), err
}
