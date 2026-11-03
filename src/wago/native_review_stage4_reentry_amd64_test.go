//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

// Unlike an escaping trap, a swallowed nested trap resumes a live outer frame.
// Allocation pressure after cancellation must keep that frame's handle alive.
func TestNativeReviewStage4CaughtNestedTrap(t *testing.T) {
	for _, shift := range []uint8{0, 6, 10} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("dead-gc-new", false), gcHostReentryFrameRootModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			var in *Instance
			active := false
			trap := errors.New("native review caught nested trap")
			host := CallerHostCallFunc(func(caller Caller, call HostCall) {
				if active {
					panic(HostTrap{Err: trap})
				}
				active = true
				defer func() { active = false }()
				if _, err := in.InvokeFromHost(context.Background(), caller, "outer", 92); !errors.Is(err, trap) {
					panic(fmt.Sprintf("nested trap: %v", err))
				}
				if shift != 0 && nativeRegionState(in, gc.NativeStructAllocCountOffset) != 0 {
					panic("nested trap retained reservation")
				}
				if _, err := in.InvokeFromHost(context.Background(), caller, "inner"); err != nil {
					panic(err)
				}
				call.ResultSlots()[0] = 0
			})
			in, err = Instantiate(compiled, InstantiateOptions{GC: GCConfig{NurseryBytes: 2048, NurseryRegionShift: shift, VerifyAfterCollect: true}, Imports: testImports("env.reenter", host)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := uint64(0); i < 20; i++ {
				got, err := in.Invoke("outer", 91+i)
				if err != nil || len(got) != 1 || got[0] != 91+i {
					t.Fatalf("resumed caller %v %v", got, err)
				}
				assertHostReentryDepthsEmpty(t)
			}
			if stats := in.gc.Stats(); stats.FullCollections+stats.MinorCollections == 0 {
				t.Fatal("no collection pressure")
			}
		})
	}
}
