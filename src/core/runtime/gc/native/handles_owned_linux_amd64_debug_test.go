//go:build linux && amd64 && wagodebug && !tinygo && !wago_target_tinygo

package gc

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestOwnedHandleReleaseFailureRetainsOwner(t *testing.T) {
	c := newTestCollector(t, Config{DisableCollection: true, NurseryBytes: 4096, ThroughputHeapBytes: 1 << 20})
	c.UseOwnedThroughputBacking()
	if _, err := c.NewStruct(0); err != nil {
		t.Fatal(err)
	}
	if c.handlesBacking != handlesBackingMapped {
		t.Fatal("mapped fixture required")
	}
	base := unsafe.SliceData(c.handles)
	full := c.mappedHandleBacking()
	cleanup := armFailure(c, failHandleBackingRelease, 0)
	defer cleanup()
	func() {
		defer func() {
			if got := recover(); got == nil || !strings.Contains(fmt.Sprint(got), "cannot release handle backing") || !strings.Contains(fmt.Sprint(got), errInjectedFailure.Error()) {
				t.Fatalf("wrong release failure: %v", got)
			}
		}()
		c.Close()
	}()
	if c.handlesBacking != handlesBackingMapped || unsafe.SliceData(c.handles) != base {
		t.Fatal("release error discarded the original owner")
	}
	cleanup()
	c.Close()
	if c.handlesBacking != handlesBackingDefault || c.handles != nil {
		t.Fatal("retry retained owner after completed release")
	}
	if err := syscall.Munmap(full); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("full reservation remains registered: %v", err)
	}
}
