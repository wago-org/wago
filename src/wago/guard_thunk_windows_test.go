//go:build windows && (amd64 || arm64) && wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
)

func TestWindowsGuardCommitCallingConventions(t *testing.T) {
	option := "frame-elide"
	if runtime.GOARCH == "arm64" {
		option = "reg-abi"
	}
	for _, elide := range []bool{false, true} {
		t.Run(fmt.Sprintf("%s=%v", option, elide), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased).WithOptimization(option, elide), growMemModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("grow", 2); err != nil {
				t.Fatal(err)
			}
			// Touch newly grown pages in native code before a host accessor commits them.
			for _, offset := range []uint64{65536, 3*65536 - 4} {
				if _, err := in.Invoke("store", offset, 42); err != nil {
					t.Fatal(err)
				}
			}
			data := in.Memory().UnsafeBytes()
			for _, offset := range []int{65536, 3*65536 - 4} {
				if got := binary.LittleEndian.Uint32(data[offset:]); got != 42 {
					t.Fatalf("page value = %d", got)
				}
			}
		})
	}
}
