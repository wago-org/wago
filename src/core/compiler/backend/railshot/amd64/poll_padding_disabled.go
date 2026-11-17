//go:build amd64 && !wago_experimental_poll_padding

package amd64

func (f *fn) padInterruptPoll(start int) {}

func interruptPollPaddingWidth() int { return 0 }
