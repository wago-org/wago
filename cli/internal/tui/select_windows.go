//go:build windows

package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/wago-org/wago/cli/internal/automation"
)

const (
	windowsKeyEvent        = 0x0001
	windowsVKReturn        = 0x0d
	windowsVKEscape        = 0x1b
	windowsVKLeft          = 0x25
	windowsVKUp            = 0x26
	windowsVKRight         = 0x27
	windowsVKDown          = 0x28
	windowsEnableProcessed = 0x0001
	windowsEnableLine      = 0x0002
	windowsEnableEcho      = 0x0004
	windowsEnableVTOutput  = 0x0004
)

var (
	windowsKernel32                   = syscall.NewLazyDLL("kernel32.dll")
	windowsGetConsoleMode             = windowsKernel32.NewProc("GetConsoleMode")
	windowsSetConsoleMode             = windowsKernel32.NewProc("SetConsoleMode")
	windowsReadConsoleInput           = windowsKernel32.NewProc("ReadConsoleInputW")
	selectorOutput          io.Writer = os.Stderr
	openSelectorConsole               = openWindowsSelectorConsole
)

type windowsInputRecord struct {
	EventType uint16
	_         uint16
	Key       windowsKeyEventRecord
}

type windowsKeyEventRecord struct {
	Down         int32
	Repeat       uint16
	VirtualKey   uint16
	VirtualScan  uint16
	UnicodeChar  uint16
	ControlState uint32
}

type selectorConsole interface {
	readKey() (selectKey, bool)
	close()
}

type windowsSelectorConsole struct {
	input     syscall.Handle
	inputMode uint32
}

func Run(m selectorModel) (submitted, cancelled bool) {
	if automation.NoInput() {
		return false, false
	}
	console, interactive := openSelectorConsole()
	if !interactive {
		return false, false
	}
	defer console.close()
	enableWindowsVirtualTerminal()

	previousLines := 0
	paint := func() {
		if previousLines > 0 {
			fmt.Fprintf(selectorOutput, "\x1b[%dA\x1b[J", previousLines)
		}
		frame := m.frame()
		fmt.Fprint(selectorOutput, strings.ReplaceAll(frame, "\n", "\r\n"))
		previousLines = strings.Count(frame, "\n")
	}
	clear := func() {
		if previousLines > 0 {
			fmt.Fprintf(selectorOutput, "\x1b[%dA\x1b[J", previousLines)
			previousLines = 0
		}
	}

	paint()
	for {
		key, ok := console.readKey()
		if !ok {
			clear()
			return false, true
		}
		done, cancel := m.apply(key)
		paint()
		if done {
			clear()
			return !cancel, cancel
		}
	}
}

func StdinIsTTY() bool {
	if automation.NoInput() {
		return false
	}
	var mode uint32
	result, _, _ := windowsGetConsoleMode.Call(
		uintptr(syscall.Handle(os.Stdin.Fd())),
		uintptr(unsafe.Pointer(&mode)),
	)
	return result != 0
}

func openWindowsSelectorConsole() (selectorConsole, bool) {
	console := &windowsSelectorConsole{input: syscall.Handle(os.Stdin.Fd())}
	result, _, _ := windowsGetConsoleMode.Call(
		uintptr(console.input),
		uintptr(unsafe.Pointer(&console.inputMode)),
	)
	if result == 0 {
		return nil, false
	}
	mode := console.inputMode &^ (windowsEnableProcessed | windowsEnableLine | windowsEnableEcho)
	result, _, _ = windowsSetConsoleMode.Call(uintptr(console.input), uintptr(mode))
	if result == 0 {
		return nil, false
	}
	return console, true
}

func (c *windowsSelectorConsole) close() {
	windowsSetConsoleMode.Call(uintptr(c.input), uintptr(c.inputMode))
}

func (c *windowsSelectorConsole) readKey() (selectKey, bool) {
	for {
		var record windowsInputRecord
		var count uint32
		result, _, _ := windowsReadConsoleInput.Call(
			uintptr(c.input),
			uintptr(unsafe.Pointer(&record)),
			1,
			uintptr(unsafe.Pointer(&count)),
		)
		if result == 0 {
			return keyNoop, false
		}
		if count == 0 || record.EventType != windowsKeyEvent || record.Key.Down == 0 {
			continue
		}
		switch record.Key.VirtualKey {
		case windowsVKReturn:
			return keyAccept, true
		case windowsVKEscape:
			return keyCancel, true
		case windowsVKLeft:
			return keyLeft, true
		case windowsVKUp:
			return keyUp, true
		case windowsVKRight:
			return keyRight, true
		case windowsVKDown:
			return keyDown, true
		}
		if record.Key.UnicodeChar != 0 {
			r := utf16.Decode([]uint16{record.Key.UnicodeChar})[0]
			return decodeKey([]byte(string(r))), true
		}
	}
}

func enableWindowsVirtualTerminal() {
	output := syscall.Handle(os.Stderr.Fd())
	var mode uint32
	if result, _, _ := windowsGetConsoleMode.Call(
		uintptr(output),
		uintptr(unsafe.Pointer(&mode)),
	); result != 0 {
		windowsSetConsoleMode.Call(uintptr(output), uintptr(mode|windowsEnableVTOutput))
	}
}
