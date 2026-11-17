//go:build amd64 && wago_experimental_poll_padding

package amd64

import "os"

// Startup selection allows the same diagnostic binary to compare both modes.
var interruptPollPadding = parseInterruptPollPadding(os.Getenv("WAGO_AMD64_POLL_PADDING"))

func parseInterruptPollPadding(value string) int {
	switch value {
	case "", "0":
		return 0
	case "32":
		return 32
	default:
		panic("WAGO_AMD64_POLL_PADDING must be 0 or 32")
	}
}

func (f *fn) padInterruptPoll(start int) {
	f.padInterruptPollTo(start, interruptPollPadding)
}

func (f *fn) padInterruptPollTo(start, width int) {
	if width == 0 {
		return
	}
	// Relative size, not absolute-address alignment. NOP leaves registers, flags,
	// memory and trap bookkeeping unchanged on the original fall-through path.
	n := (width - (f.a.Len()-start)%width) % width
	for ; n > 0; n-- {
		f.a.EmitBytes([]byte{0x90})
	}
}

func interruptPollPaddingWidth() int { return interruptPollPadding }
