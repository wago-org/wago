//go:build linux && amd64

package main

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/wago-org/wago/internal/jitprofile"
	"github.com/wago-org/wago/profile"
	"golang.org/x/sys/unix"
)

//go:embed unwind.hex
var unwindHex string

func run(code, sp uintptr, depth uint64)
func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	if len(os.Args) != 3 || (os.Args[2] != "none" && os.Args[2] != "offline" && os.Args[2] != "mapped" && os.Args[2] != "rows") {
		panic("usage: unwindprobe NEW_DIRECTORY none|offline|mapped|rows (set GODEBUG=asyncpreemptoff=1)")
	}
	if os.Getenv("GODEBUG") != "asyncpreemptoff=1" {
		panic("fixture requires GODEBUG=asyncpreemptoff=1")
	}
	runtime.LockOSThread()
	unwind, err := hex.DecodeString(strings.TrimSpace(unwindHex))
	must(err)
	dir := os.Args[1]
	must(os.Mkdir(dir, 0700))
	// Nine recursive activations, each with an eight-byte RSP allocation.
	// The leaf spins; no frame pointer or Go callback is used in generated code.
	code := []byte{0x48, 0x83, 0xec, 8, 0x85, 0xff, 0x74, 12, 0xff, 0xcf, 0xe8, 0xf1, 0xff, 0xff, 0xff, 0x48, 0x83, 0xc4, 8, 0xc3, 0xb9, 0xa0, 0x86, 1, 0, 0xff, 0xc9, 0x75, 0xfc, 0x48, 0x83, 0xc4, 8, 0xc3}
	mem, err := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	must(err)
	if os.Args[2] == "mapped" {
		copy(mem[40:], unwind)
	}
	copy(mem, code)
	must(unix.Mprotect(mem, unix.PROT_READ|unix.PROT_EXEC))
	defer unix.Munmap(mem)
	stack, err := unix.Mmap(-1, 0, 128<<10, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	must(err)
	defer unix.Munmap(stack)
	f, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("jit-%d.dump", os.Getpid())), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	must(err)
	defer f.Close()
	dump, err := profile.NewJITDump(f, uint32(os.Getpid()), "amd64", jitprofile.Now())
	must(err)
	marker, err := unix.Mmap(int(f.Fd()), 0, 4096, unix.PROT_READ|unix.PROT_EXEC, unix.MAP_PRIVATE)
	must(err)
	defer unix.Munmap(marker)
	addr := uintptr(unsafe.Pointer(&mem[0]))
	if os.Args[2] != "none" && os.Args[2] != "rows" {
		data := unwind
		info := profile.UnwindInfo{EHFrame: data[:len(data)-20], EHFrameHeader: data[len(data)-20:]}
		if os.Args[2] == "mapped" {
			info.MappedSize = uint64(len(data))
		}
		must(dump.WriteUnwind(1, uint64(addr), uint64(len(code)), info, jitprofile.Now()))
	}
	sp := uintptr(unsafe.Pointer(&stack[len(stack)-8192])) &^ 15
	im := jitprofile.Image{ID: 1, ModuleID: "probe", ArtifactID: "recursive-cfi", Base: uint64(addr), Size: uint64(len(code)), Target: "linux/amd64", Code: code, Regions: []jitprofile.Region{{Size: uint64(len(code)), Kind: "guest-body", Function: 0, Name: "recursive_probe"}}}
	if os.Args[2] == "rows" {
		im.Unwind = []jitprofile.UnwindRange{
			{Offset: 0, Size: 4, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
			{Offset: 4, Size: 15, CFARegister: 7, CFAOffset: 16, ReturnOffset: -8},
			{Offset: 19, Size: 1, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
			{Offset: 20, Size: 13, CFARegister: 7, CFAOffset: 16, ReturnOffset: -8},
			{Offset: 33, Size: 1, CFARegister: 7, CFAOffset: 8, ReturnOffset: -8},
		}
	}
	must(dump.Write([]jitprofile.Event{{Kind: "load", ImageID: 1, Timestamp: jitprofile.Now(), Image: &im}}))
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		run(addr, sp, 8)
	}
	must(dump.Close(jitprofile.Now()))
}
