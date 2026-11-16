//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

// Retargeting the exact root simulates the address rewrite performed by a
// moving direct-reference collector. The issuance snapshot must not override it.
func TestGCTokenBoundariesUseAuthoritativeRoot(t *testing.T) {
	for _, boundary := range []string{"argument", "global", "host-result"} {
		t.Run(boundary, func(t *testing.T) {
			compiled, in := instantiateStagedGCStructBasic(t, GCConfig{})
			defer compiled.Close()
			defer in.Close()
			token, entry := issueStagedGCStructToken(t, in)
			moved, err := in.gc.NewStructDefaultWithRoots(0, gc.EmptyRoots{})
			if err != nil {
				t.Fatal(err)
			}
			if moved == entry.ref {
				t.Fatal("fixture needs a distinct rewritten reference")
			}
			if err = in.gc.SetGlobalSlot(entry.slot, moved); err != nil {
				t.Fatal(err)
			}
			if err = in.gc.CollectFull(gc.EmptyRoots{}); err != nil {
				t.Fatal(err)
			}
			var invoke func() (gc.Ref, error)
			switch boundary {
			case "argument":
				invoke = func() (gc.Ref, error) { return in.refStore.stageGCRefArgument(in, token.token, entry.exact) }
			case "global":
				slot, e := in.gc.NewCheckedGlobalSlot(gc.Null())
				if e != nil {
					t.Fatal(e)
				}
				invoke = func() (gc.Ref, error) {
					unlock := lockNativeExecutionForHostAccess()
					defer unlock()
					return in.refStore.publishGCRefGlobal(in, token.token, entry.exact, slot)
				}
			case "host-result":
				state := in.publicGCState()
				state.hostActivationCount = 1
				state.hostActivations[0].stack = &coreruntime.Engine{}
				defer func() { state.hostActivations[0] = gcHostActivation{}; state.hostActivationCount = 0 }()
				parked := gcHostActivationToken{state: state, index: 0}
				invoke = func() (gc.Ref, error) { return in.refStore.stageGCHostResult(in, parked, token.token, entry.exact) }
			}
			got, err := invoke()
			if err != nil || got != moved {
				t.Fatalf("boundary got %v,%v; want rewritten root %v", got, err, moved)
			}
			if err = in.gc.SetGlobalSlot(entry.slot, gc.Null()); err != nil {
				t.Fatal(err)
			}
			if _, err = invoke(); err == nil {
				t.Fatal("boundary accepted a token with a cleared root")
			}
		})
	}
}
