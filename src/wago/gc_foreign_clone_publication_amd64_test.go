//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func newClonePublicationFixture(t testing.TB, config GCConfig) (*Instance, *Instance, GCRef) {
	t.Helper()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	sourceRT, sourceHost, sourceModule, source := instantiateForeignCloneFixture(t, cfg, config)
	targetRT, targetHost, targetModule, target := instantiateForeignCloneFixture(t, cfg, config)
	t.Cleanup(func() {
		target.Close()
		targetModule.Close()
		targetHost.Close()
		targetRT.Close()
		source.Close()
		sourceModule.Close()
		sourceHost.Close()
		sourceRT.Close()
	})
	values, err := source.InvokeValues(context.Background(), "new")
	if err != nil || len(values) != 1 {
		t.Fatalf("source new = %v, %v", values, err)
	}
	token := values[0].GCRef()
	t.Cleanup(func() { source.ReleaseGCRef(token) })
	return source, target, token
}

func TestForeignClonePublicationLockedHelpersSerializeCollection(t *testing.T) {
	for _, profile := range []struct {
		name   string
		config GCConfig
	}{
		{"throughput", GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			source, target, sourceToken := newClonePublicationFixture(t, profile.config)
			objects, root, err := captureForeignGCGraph(source, sourceToken.token, target)
			if err != nil {
				t.Fatal(err)
			}
			reconstructed := make(chan struct{})
			publish := make(chan struct{})
			type result struct {
				token GCRef
				err   error
			}
			firstResult := make(chan result, 1)
			secondResult := make(chan result, 1)
			go func() {
				invocation := target.lockGCInvocation(newInvocationID())
				defer invocation.unlock()
				unlockNative := target.lockInstanceNativeStateForHostAccess()
				domain := target.lockGCCollector()
				state := target.publicGCState()
				state.mu.Lock()
				defer func() { state.mu.Unlock(); unlockGCCollector(domain); unlockNative() }()
				ref, localType, err := restoreForeignGCGraphLocked(target, state, objects, root)
				if err != nil {
					close(reconstructed)
					firstResult <- result{err: err}
					return
				}
				close(reconstructed)
				<-publish
				required := ValueTypeDescriptor{Kind: ValueTypeReference, Ref: ReferenceTypeDescriptor{Exact: true, Heap: HeapTypeDescriptor{Defined: true, TypeIndex: localType}}}
				token, err := target.refStore.issueGCRefLocked(target, state, ref, required)
				clearForeignCloneRootLocked(target, state, err != nil)
				firstResult <- result{GCRef{token: token}, err}
			}()
			<-reconstructed
			// Pause the locked helper sequence in its unpublished phase. This
			// checks the helper contract; the public API is exercised separately.
			if nativeExecutionMu.TryLock() {
				nativeExecutionMu.Unlock()
				close(publish)
				t.Fatal("native lease was released before token publication")
			}
			state := target.existingPublicGCState()
			if state.mu.TryLock() {
				state.mu.Unlock()
				close(publish)
				t.Fatal("target state was released before token publication")
			}
			secondStarted := make(chan struct{})
			go func() {
				close(secondStarted)
				token, err := target.CloneGCRefFrom(source, sourceToken)
				secondResult <- result{token, err}
			}()
			collectionStarted := make(chan struct{})
			collected := make(chan error, 1)
			go func() { close(collectionStarted); collected <- target.CollectGC() }()
			<-secondStarted
			<-collectionStarted
			select {
			case <-secondResult:
				close(publish)
				t.Fatal("competing clone passed the publication barrier")
			default:
			}
			select {
			case <-collected:
				close(publish)
				t.Fatal("collection passed the publication barrier")
			default:
			}
			close(publish)
			a, b := <-firstResult, <-secondResult
			if a.err != nil || b.err != nil || a.token.IsNull() || b.token.IsNull() || a.token == b.token {
				t.Fatalf("clone results = %+v, %+v", a, b)
			}
			if err := <-collected; err != nil {
				t.Fatal(err)
			}
			if err := target.CollectGC(); err != nil {
				t.Fatal(err)
			}
			for _, token := range []GCRef{a.token, b.token} {
				got, err := target.InvokeValues(context.Background(), "read", ValueGCRef(token))
				if err != nil || len(got) != 1 || got[0].I32() != 42 {
					t.Fatalf("published clone = %v, %v", got, err)
				}
				target.refStore.mu.Lock()
				entry := target.refStore.gcByToken[token.token]
				target.refStore.mu.Unlock()
				if entry.owner != target {
					t.Fatal("clone token has the wrong owner")
				}
				if err := target.ReleaseGCRef(token); err != nil {
					t.Fatal(err)
				}
			}
			if !target.gc.GlobalSlot(state.cloneRootSlot).IsNull() || state.resultTokenCount != 0 {
				t.Fatal("cleanup retained a clone root or token")
			}
			if err := target.CollectGC(); err != nil {
				t.Fatal(err)
			}
			if live := target.gc.Stats().LiveObjects; live != 0 {
				t.Fatalf("released clone objects = %d, want zero", live)
			}
		})
	}
}

func TestForeignClonePublicationFailureClearsPrivateRoot(t *testing.T) {
	for _, config := range []GCConfig{{CollectEveryAlloc: true}, {Profile: GCProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16, TinyCollectEveryAlloc: true}} {
		source, target, token := newClonePublicationFixture(t, config)
		objects, root, err := captureForeignGCGraph(source, token.token, target)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			invocation := target.lockGCInvocation(newInvocationID())
			defer invocation.unlock()
			unlockNative := target.lockInstanceNativeStateForHostAccess()
			defer unlockNative()
			domain := target.lockGCCollector()
			defer unlockGCCollector(domain)
			state := target.publicGCState()
			state.mu.Lock()
			defer state.mu.Unlock()
			ref, _, err := restoreForeignGCGraphLocked(target, state, objects, root)
			if err != nil {
				t.Fatal(err)
			}
			// A rejected publication follows the same locked cleanup path as a closed
			// producer or token-creation error, without a test hook in production code.
			_, err = target.refStore.issueGCRefLocked(target, state, ref, ValueTypeDescriptor{})
			if err == nil {
				t.Fatal("invalid result type was published")
			}
			clearForeignCloneRootLocked(target, state, true)
			if !target.gc.GlobalSlot(state.cloneRootSlot).IsNull() || state.resultTokenCount != 0 || target.gc.Stats().LiveObjects != 0 {
				t.Fatal("failed publication leaked ownership")
			}
		}()
		objects = append(objects, gcCloneObject{typeID: ^gc.TypeID(0)})
		invocation := target.lockGCInvocation(newInvocationID())
		_, _, err = restoreForeignGCGraph(target, objects, root)
		invocation.unlock()
		if err == nil {
			t.Fatal("invalid trailing object did not fail reconstruction")
		}
		state := target.existingPublicGCState()
		if !target.gc.GlobalSlot(state.cloneRootSlot).IsNull() || state.resultTokenCount != 0 || target.gc.Stats().LiveObjects != 0 {
			t.Fatal("failed reconstruction leaked ownership")
		}
		// The standalone cleanup entry point must also remain safe after a
		// failed reconstruction has already cleared its temporary ownership.
		invocation = target.lockGCInvocation(newInvocationID())
		clearForeignCloneRoot(target, true)
		invocation.unlock()
		if !target.gc.GlobalSlot(state.cloneRootSlot).IsNull() || state.resultTokenCount != 0 || target.gc.Stats().LiveObjects != 0 {
			t.Fatal("repeated cleanup changed ownership after failed reconstruction")
		}
	}
}

func TestForeignClonePublicationConcurrentPublicOperations(t *testing.T) {
	for _, profile := range []struct {
		name   string
		config GCConfig
	}{
		{"throughput", GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			source, target, sourceToken := newClonePublicationFixture(t, profile.config)
			const count = 32
			seeds := make([]GCRef, count)
			t.Cleanup(func() {
				for _, token := range seeds {
					if !token.IsNull() {
						_ = target.ReleaseGCRef(token)
					}
				}
			})
			for i := range seeds {
				var err error
				seeds[i], err = target.CloneGCRefFrom(source, sourceToken)
				if err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			done := make(chan error, 4)
			cloned := make(chan GCRef, count)
			for worker := 0; worker < 2; worker++ {
				go func() {
					<-start
					for i := 0; i < count/2; i++ {
						token, err := target.CloneGCRefFrom(source, sourceToken)
						if err != nil {
							done <- err
							return
						}
						cloned <- token
					}
					done <- nil
				}()
			}
			go func() {
				<-start
				for i := 0; i < count; i++ {
					if err := target.CollectGC(); err != nil {
						done <- err
						return
					}
				}
				done <- nil
			}()
			go func() {
				<-start
				for i, token := range seeds {
					if err := target.ReleaseGCRef(token); err != nil {
						done <- err
						return
					}
					seeds[i] = GCRef{}
				}
				done <- nil
			}()
			close(start)
			for i := 0; i < 4; i++ {
				if err := <-done; err != nil {
					t.Error(err)
				}
			}
			close(cloned)
			for token := range cloned {
				got, err := target.InvokeValues(context.Background(), "read", ValueGCRef(token))
				if err != nil || len(got) != 1 || got[0].I32() != 42 {
					t.Errorf("published clone = %v, %v", got, err)
				}
				if err := target.ReleaseGCRef(token); err != nil {
					t.Error(err)
					t.Cleanup(func() { _ = target.ReleaseGCRef(token) })
				}
			}
			if err := target.CollectGC(); err != nil {
				t.Fatal(err)
			}
			state := target.existingPublicGCState()
			if !target.gc.GlobalSlot(state.cloneRootSlot).IsNull() || state.resultTokenCount != 0 || target.gc.Stats().LiveObjects != 0 {
				t.Fatal("concurrent operations leaked clone ownership")
			}
		})
	}
}

func TestForeignClonePublicationClosedTargetClearsPrivateRoot(t *testing.T) {
	for _, profile := range []struct {
		name   string
		config GCConfig
	}{
		{"throughput", GCConfig{CollectEveryAlloc: true, VerifyAfterCollect: true}},
		{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 1024, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			source, target, sourceToken := newClonePublicationFixture(t, profile.config)
			retained, err := target.CloneGCRefFrom(source, sourceToken)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if !retained.IsNull() {
					_ = target.ReleaseGCRef(retained)
				}
			})
			// Keep the collector alive through logical close so rejection occurs
			// at public ownership publication, after reconstruction has succeeded.
			if err := target.Close(); err != nil {
				t.Fatal(err)
			}
			cloned, err := target.CloneGCRefFrom(source, sourceToken)
			if err == nil || !cloned.IsNull() {
				t.Fatalf("clone into closed target = %v, %v", cloned, err)
			}
			state := target.existingPublicGCState()
			if !target.gc.GlobalSlot(state.cloneRootSlot).IsNull() || state.resultTokenCount != 1 || target.gc.Stats().LiveObjects != 1 {
				t.Fatal("failed publication changed existing ownership or leaked the clone")
			}
			if refs := target.referenceLifetime().snapshot().ResourceRoots; refs != 1 {
				t.Fatalf("resource roots = %d, want one retained token", refs)
			}
			if err := target.ReleaseGCRef(retained); err != nil {
				t.Fatal(err)
			}
			retained = GCRef{}
			if target.hasPhysicalResources() {
				t.Fatal("closed target remained retained after the final token release")
			}
		})
	}
}

func BenchmarkForeignClonePublication(b *testing.B) {
	for _, profile := range []struct {
		name   string
		config GCConfig
	}{{"throughput", GCConfig{}}, {"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}}} {
		b.Run(profile.name, func(b *testing.B) {
			source, target, token := newClonePublicationFixture(b, profile.config)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cloned, err := target.CloneGCRefFrom(source, token)
				if err != nil {
					b.Fatal(err)
				}
				if err := target.ReleaseGCRef(cloned); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
