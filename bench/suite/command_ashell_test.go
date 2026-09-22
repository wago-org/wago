package wagobench

import (
	"encoding/binary"
	"testing"
)

const (
	// WASI Preview 1 errno values. The a-Shell imports use the same errno ABI.
	ashellErrnoSuccess = 0
	ashellErrnoFault   = 21
	ashellErrnoNosys   = 52
	ashellErrnoRange   = 68
)

// ashellGetcwd exposes the virtual preopen root, never the host checkout path.
// a-Shell's import writes the path bytes and their count; its libc adds NUL.
func ashellGetcwd(memory []byte, buf, bufLen, used uint32) uint32 {
	if uint64(used)+4 > uint64(len(memory)) || uint64(buf)+uint64(bufLen) > uint64(len(memory)) {
		return ashellErrnoFault
	}
	if bufLen < 1 {
		return ashellErrnoRange
	}
	memory[buf] = '/'
	binary.LittleEndian.PutUint32(memory[used:], 1)
	return ashellErrnoSuccess
}

// The corpus supplies no guest environment variables. a-Shell's getenv import
// signals an absent value by writing a zero count, not by returning an error.
func ashellGetenv(memory []byte, name, nameLen, buf, bufLen, used uint32) uint32 {
	if uint64(name)+uint64(nameLen) > uint64(len(memory)) ||
		uint64(buf)+uint64(bufLen) > uint64(len(memory)) ||
		uint64(used)+4 > uint64(len(memory)) {
		return ashellErrnoFault
	}
	binary.LittleEndian.PutUint32(memory[used:], 0)
	return ashellErrnoSuccess
}

func TestAShellGetcwd(t *testing.T) {
	memory := make([]byte, 16)
	if got := ashellGetcwd(memory, 2, 4, 8); got != ashellErrnoSuccess {
		t.Fatalf("getcwd errno = %d", got)
	}
	if memory[2] != '/' || binary.LittleEndian.Uint32(memory[8:]) != 1 {
		t.Fatalf("getcwd memory = %v", memory)
	}
	if got := ashellGetcwd(memory, 16, 1, 8); got != ashellErrnoFault {
		t.Fatalf("out-of-bounds buffer errno = %d", got)
	}
	if got := ashellGetcwd(memory, 2, 1, 14); got != ashellErrnoFault {
		t.Fatalf("out-of-bounds count errno = %d", got)
	}
	if got := ashellGetcwd(memory, 2, 0, 8); got != ashellErrnoRange {
		t.Fatalf("empty buffer errno = %d", got)
	}
}

func TestAShellGetenv(t *testing.T) {
	memory := make([]byte, 16)
	binary.LittleEndian.PutUint32(memory[8:], 99)
	if got := ashellGetenv(memory, 0, 4, 4, 4, 8); got != ashellErrnoSuccess {
		t.Fatalf("getenv errno = %d", got)
	}
	if used := binary.LittleEndian.Uint32(memory[8:]); used != 0 {
		t.Fatalf("getenv used = %d", used)
	}
	if got := ashellGetenv(memory, 16, 1, 4, 4, 8); got != ashellErrnoFault {
		t.Fatalf("out-of-bounds name errno = %d", got)
	}
}
