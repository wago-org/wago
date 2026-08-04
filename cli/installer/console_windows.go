package main

import (
	"fmt"
	"os"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	keyEvent       = 0x0001
	vkBack         = 0x08
	vkTab          = 0x09
	vkReturn       = 0x0d
	vkEscape       = 0x1b
	vkLeft         = 0x25
	vkUp           = 0x26
	vkRight        = 0x27
	vkDown         = 0x28
	enableEcho     = 0x0004
	enableLine     = 0x0002
	enableVTOutput = 0x0004
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode   = kernel32.NewProc("GetConsoleMode")
	setConsoleMode   = kernel32.NewProc("SetConsoleMode")
	readConsoleInput = kernel32.NewProc("ReadConsoleInputW")
	getConsoleInfo   = kernel32.NewProc("GetConsoleScreenBufferInfo")
	setCursor        = kernel32.NewProc("SetConsoleCursorPosition")
	fillCharacters   = kernel32.NewProc("FillConsoleOutputCharacterW")
	fillAttributes   = kernel32.NewProc("FillConsoleOutputAttribute")
)

type coord struct {
	X int16
	Y int16
}

type smallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

type inputRecord struct {
	EventType uint16
	_         uint16
	Key       keyEventRecord
}

type keyEventRecord struct {
	Down         int32
	Repeat       uint16
	VirtualKey   uint16
	VirtualScan  uint16
	UnicodeChar  uint16
	ControlState uint32
}

type console struct {
	input     syscall.Handle
	inputMode uint32
}

func openConsole() (*console, bool) {
	c := &console{input: syscall.Handle(os.Stdin.Fd())}
	result, _, _ := getConsoleMode.Call(uintptr(c.input), uintptr(unsafe.Pointer(&c.inputMode)))
	if result == 0 {
		return nil, false
	}
	mode := c.inputMode &^ (enableEcho | enableLine)
	result, _, _ = setConsoleMode.Call(uintptr(c.input), uintptr(mode))
	if result == 0 {
		return nil, false
	}
	enableVirtualTerminal()
	return c, true
}

func (c *console) close() {
	setConsoleMode.Call(uintptr(c.input), uintptr(c.inputMode))
}

func (c *console) readKey() key {
	for {
		var record inputRecord
		var count uint32
		result, _, _ := readConsoleInput.Call(
			uintptr(c.input),
			uintptr(unsafe.Pointer(&record)),
			1,
			uintptr(unsafe.Pointer(&count)),
		)
		if result == 0 || count == 0 || record.EventType != keyEvent || record.Key.Down == 0 {
			continue
		}
		switch record.Key.VirtualKey {
		case vkBack:
			return key{name: "backspace"}
		case vkTab:
			return key{name: "tab"}
		case vkReturn:
			return key{name: "enter"}
		case vkEscape:
			return key{name: "escape"}
		case vkLeft:
			return key{name: "left"}
		case vkUp:
			return key{name: "up"}
		case vkRight:
			return key{name: "right"}
		case vkDown:
			return key{name: "down"}
		}
		if record.Key.UnicodeChar != 0 {
			r := utf16.Decode([]uint16{record.Key.UnicodeChar})[0]
			return key{rune: r}
		}
	}
}

func stderrIsConsole() bool {
	var mode uint32
	result, _, _ := getConsoleMode.Call(uintptr(syscall.Handle(os.Stderr.Fd())), uintptr(unsafe.Pointer(&mode)))
	return result != 0
}

func enableVirtualTerminal() {
	var mode uint32
	output := syscall.Handle(os.Stderr.Fd())
	if result, _, _ := getConsoleMode.Call(uintptr(output), uintptr(unsafe.Pointer(&mode))); result != 0 {
		setConsoleMode.Call(uintptr(output), uintptr(mode|enableVTOutput))
	}
}

func clearProgressLine() {
	clearWindowsConsole(0)
}

func clearConsoleLines(lines int) {
	if lines > 0 {
		clearWindowsConsole(lines)
	}
}

func clearWindowsConsole(lines int) {
	output := syscall.Handle(os.Stderr.Fd())
	var info consoleScreenBufferInfo
	if result, _, _ := getConsoleInfo.Call(uintptr(output), uintptr(unsafe.Pointer(&info))); result == 0 {
		if lines == 0 {
			fmt.Fprint(os.Stderr, "\r\x1b[2K")
		} else {
			fmt.Fprintf(os.Stderr, "\x1b[%dA\x1b[J", lines)
		}
		return
	}
	start := coord{Y: info.CursorPosition.Y - int16(lines)}
	if start.Y < 0 {
		start.Y = 0
	}
	rows := info.CursorPosition.Y - start.Y + 1
	cells := uint32(info.Size.X) * uint32(rows)
	position := uintptr(uint16(start.X)) | uintptr(uint32(uint16(start.Y)))<<16
	var written uint32
	fillCharacters.Call(uintptr(output), uintptr(' '), uintptr(cells), position, uintptr(unsafe.Pointer(&written)))
	fillAttributes.Call(uintptr(output), uintptr(info.Attributes), uintptr(cells), position, uintptr(unsafe.Pointer(&written)))
	setCursor.Call(uintptr(output), position)
}
