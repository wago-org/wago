//go:build linux && amd64 && wago_gc_compact_shared_checks

package wago

import (
	"encoding/binary"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"testing"
	"unsafe"
)

// Common metadata still has to be checked once, and the child retains its own
// exact canonical type comparison. These corruptions must select the checked Go
// helper rather than allowing the native store to bypass the failed guard.
func TestCompactNativeSharedStoreChecks(t *testing.T) {
	for _, kind := range []string{"instance-version", "instance-size", "collector-version", "collector-size", "map-count", "child-canonical-type"} {
		t.Run(kind, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactRefStoreModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			in, err := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, NurseryBytes: 512, ThroughputHeapBytes: 64 << 10}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err = in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			view, iv := in.gc.CompactView(), in.compactState.view
			saved, savedIV := *view, *iv
			types := unsafe.Slice((*uint32)(unsafe.Pointer(iv.LocalTypes)), int(iv.LocalTypeCount))
			savedType := types[0]
			restore := func() { *view = saved; *iv = savedIV; types[0] = savedType }
			defer restore()
			switch kind {
			case "instance-version":
				iv.Version++
			case "instance-size":
				iv.Size = 0
			case "collector-version":
				view.Version++
			case "collector-size":
				view.Size = 0
			case "map-count":
				iv.LocalTypeCount = 0
			case "child-canonical-type":
				types[0] = types[1]
			}
			const sentinel = 0x81234567
			index := in.ctrl[coreruntime.HostCtrlImportIndexOffset:]
			binary.LittleEndian.PutUint32(index, sentinel)
			got, err := in.Invoke("store", 0)
			restore()
			if err != nil || len(got) != 1 || got[0] != 7 {
				t.Fatal(got, err)
			}
			if binary.LittleEndian.Uint32(index) == sentinel {
				t.Fatal("failed metadata check did not select helper")
			}
			got, err = in.Invoke("get", 0)
			if err != nil || len(got) != 1 || got[0] != 91 {
				t.Fatal(got, err)
			}
		})
	}
}
