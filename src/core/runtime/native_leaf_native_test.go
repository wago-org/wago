//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package runtime

import (
	"bytes"
	"sync"
	"testing"

	"github.com/wago-org/wago/internal/runtimebridge"
)

func TestNativeLeafInstancesShareExecutableCode(t *testing.T) {
	controls := [][]byte{make([]byte, ctrlFrameSize), make([]byte, ctrlFrameSize)}
	for _, ctrl := range controls {
		ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), ctrl, nativeLeafIncTest, 1)
		if err != nil || !ok {
			t.Fatalf("register=%t,%v", ok, err)
		}
		defer unregisterNativeScalarLeaf(ctrl)
	}
	first := nativeScalarLeafPtr(controls[0])
	if second := nativeScalarLeafPtr(controls[1]); first != second {
		t.Fatalf("same callback allocated separate executable mappings: %x != %x", first, second)
	}
	value, _ := nativeLeafHosts.Load(slicePtr(controls[1]))
	leaf := value.(nativeScalarLeaf)
	original := append([]byte(nil), leaf.code...)
	unregisterNativeScalarLeaf(controls[0])
	if nativeScalarLeafPtr(controls[0]) != 0 || nativeScalarLeafPtr(controls[1]) != first {
		t.Fatal("closing one registration changed another registration's target")
	}
	if !bytes.Equal(leaf.code, original) {
		t.Fatal("closing one registration invalidated shared code")
	}
	if ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), controls[1], nativeLeafIncTest, 1); ok || err == nil {
		t.Fatalf("duplicate registration=%t,%v", ok, err)
	}
	if ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), controls[1], nil, 1); ok || err == nil {
		t.Fatalf("invalid callback bypassed duplicate rejection: %t,%v", ok, err)
	}
	badShape := make([]byte, ctrlFrameSize)
	if ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), badShape, nativeLeafIncTest, 2); ok || err != nil {
		unregisterNativeScalarLeaf(badShape)
		t.Fatalf("cached callback bypassed signature validation: %t,%v", ok, err)
	}
	unregisterNativeScalarLeaf(controls[1])
	nativeLeafCodeCache.Lock()
	retained := nativeLeafCodeCache.entries[leaf.owner.key] != nil
	refs := leaf.owner.refs
	nativeLeafCodeCache.Unlock()
	if retained || refs != 0 {
		t.Fatalf("last close retained executable code: cached=%t refs=%d", retained, refs)
	}
}

func TestNativeLeafConcurrentRegistrationsRetainSharedCode(t *testing.T) {
	const count = 64
	controls := make([][]byte, count)
	start := make(chan struct{})
	type result struct {
		admitted bool
		err      error
	}
	results := make(chan result, count)
	var workers sync.WaitGroup
	for i := range controls {
		controls[i] = make([]byte, ctrlFrameSize)
		defer unregisterNativeScalarLeaf(controls[i])
		workers.Add(1)
		go func(ctrl []byte) {
			defer workers.Done()
			<-start
			ok, err := RegisterNativeScalarLeaf(runtimebridge.GrantHostScalarCall(), ctrl, nativeLeafIncTest, 1)
			results <- result{ok, err}
		}(controls[i])
	}
	close(start)
	workers.Wait()
	close(results)
	for r := range results {
		if !r.admitted || r.err != nil {
			t.Fatalf("register=%t,%v", r.admitted, r.err)
		}
	}
	target := nativeScalarLeafPtr(controls[0])
	for _, ctrl := range controls {
		if nativeScalarLeafPtr(ctrl) != target {
			t.Fatal("concurrent admission created duplicate code")
		}
	}
	value, _ := nativeLeafHosts.Load(slicePtr(controls[0]))
	leaf := value.(nativeScalarLeaf)
	original := append([]byte(nil), leaf.code...)
	for _, ctrl := range controls[:count/2] {
		workers.Add(1)
		go func(ctrl []byte) { defer workers.Done(); unregisterNativeScalarLeaf(ctrl) }(ctrl)
	}
	workers.Wait()
	for _, ctrl := range controls[count/2:] {
		if nativeScalarLeafPtr(ctrl) != target {
			t.Fatal("partial close revoked a retained target")
		}
	}
	if !bytes.Equal(leaf.code, original) {
		t.Fatal("partial close changed retained executable code")
	}
}
