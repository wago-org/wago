//go:build !tinygo

package runtimeconcurrency_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	wago "github.com/wago-org/wago"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

const (
	defaultHarnessRounds = 12
	harnessTimeout       = 15 * time.Second
)

// TestRuntimeConcurrencyHarness is a black-box, seed-replayable concurrency
// workload. It intentionally uses only public Wago APIs and guest-visible
// memory/results as its oracles; scheduler controls and state probes stay in
// this test package and never enter production builds.
func TestRuntimeConcurrencyHarness(t *testing.T) {
	if (goruntime.GOOS != "linux" && goruntime.GOOS != "darwin") || (goruntime.GOARCH != "amd64" && goruntime.GOARCH != "arm64") {
		t.Skipf("runtime concurrency product is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	seeds := harnessSeeds(t)
	rounds := harnessRounds(t)
	for _, seed := range seeds {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			h := newHarness(t, seed, rounds)
			t.Cleanup(h.reportFailure)
			t.Run("threads-shared-memory-wait-close", h.testThreads)
			t.Run("host-reentry-close", h.testHostReentry)
			t.Run("gc-shared-domain-close", h.testGC)
		})
	}
}

func TestRuntimeConcurrencyGCAtomicWaitNotify(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3 | wago.CoreFeatureThreads).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	compiled, err := rt.Compile(harnessGCThreadsModule())
	if err != nil {
		t.Fatal(err)
	}
	memory, err := wago.NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	waiter, err := rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.memory": memory}))
	if err != nil {
		t.Fatal(err)
	}
	notifier, err := rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.memory": memory}))
	if err != nil {
		t.Fatal(err)
	}

	marker := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
	atomic.StoreUint32(marker, 0)
	waitDone := make(chan invokeResult, 1)
	go func() {
		out, callErr := waiter.Invoke("wait-marked")
		waitDone <- invokeResult{values: out, err: callErr}
	}()
	waitForMarker(t, marker)
	collectDone := make(chan error, 1)
	go func() { collectDone <- waiter.CollectGC() }()
	select {
	case err := <-collectDone:
		t.Fatalf("CollectGC completed while atomic.wait held the instance native lease: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	notifyDone := make(chan invokeResult, 1)
	go func() {
		out, callErr := notifier.Invoke("notify", wago.I32(8), wago.I32(1))
		notifyDone <- invokeResult{values: out, err: callErr}
	}()
	select {
	case result := <-notifyDone:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 1 {
			t.Fatalf("GC-domain atomic.notify = %v, %v; want [1], nil", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		// A waiter holding a shared collector lease prevents cleanup too, so keep
		// this failure bounded instead of obscuring it behind Close.
		t.Fatal("GC-domain atomic.notify deadlocked behind atomic.wait")
	}
	select {
	case result := <-waitDone:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 0 {
			t.Fatalf("GC-domain atomic.wait = %v, %v; want [0], nil", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("GC-domain atomic.wait did not resume")
	}
	select {
	case err := <-collectDone:
		if err != nil {
			t.Fatalf("CollectGC after atomic.wait resume: %v", err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("CollectGC remained blocked after atomic.wait resumed")
	}
	for _, in := range []*wago.Instance{waiter, notifier} {
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := memory.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConcurrencyCrossInstanceHostCollectGC(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	producerCode, err := rt.Compile(harnessGCHostProducerModule())
	if err != nil {
		t.Fatal(err)
	}
	consumerCode, err := rt.Compile(harnessGCCrossInstanceConsumerModule())
	if err != nil {
		t.Fatal(err)
	}
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	var producer *wago.Instance
	host := wago.HostFunc(func(module wago.HostModule, _ []uint64, _ []uint64) {
		collector, ok := module.(wago.GCHostModule)
		if !ok {
			panic(wago.HostTrap{Err: fmt.Errorf("cross-instance host module has no collector")})
		}
		if err := collector.CollectGC(); err != nil {
			panic(wago.HostTrap{Err: fmt.Errorf("cross-instance host collect: %w", err)})
		}
		nested, err := producer.InvokeFromHost(context.Background(), module, "nested")
		if err != nil || len(nested) != 1 || wago.AsI32(nested[0]) != 7 {
			panic(wago.HostTrap{Err: fmt.Errorf("cross-instance nested re-entry = %v, %v; want [7], nil", nested, err)})
		}
	})
	producer, err = rt.Instantiate(context.Background(), producerCode, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.collect": host}))
	if err != nil {
		t.Fatal(err)
	}
	call, err := producer.ExportedFunc("call")
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerCode, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.call": call}))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan invokeResult, 1)
	go func() {
		out, callErr := consumer.Invoke("run")
		done <- invokeResult{values: out, err: callErr}
	}()
	select {
	case result := <-done:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 1 {
			t.Fatalf("cross-instance host CollectGC = %v, %v; want [1], nil", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("cross-instance host CollectGC deadlocked")
	}
	for _, in := range []*wago.Instance{consumer, producer} {
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := consumerCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := producerCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConcurrencySameDomainHostReentry(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	gcCode, err := rt.Compile(harnessGCModule())
	if err != nil {
		t.Fatal(err)
	}
	hostCode, err := rt.Compile(harnessGCCallerModule())
	if err != nil {
		t.Fatal(err)
	}
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	first, err := rt.Instantiate(context.Background(), gcCode, wago.WithGC(gcCfg))
	if err != nil {
		t.Fatal(err)
	}
	target, err := rt.Instantiate(context.Background(), gcCode, wago.WithGC(gcCfg))
	if err != nil {
		t.Fatal(err)
	}
	created, err := first.Call(context.Background(), "new")
	if err != nil || len(created) != 1 || created[0].GCRef().IsNull() {
		t.Fatalf("new = %v, %v", created, err)
	}
	ref := created[0].GCRef()
	read, err := target.Call(context.Background(), "read", wago.ValueGCRef(ref))
	if err != nil || len(read) != 1 || read[0].I32() != 42 {
		t.Fatalf("cross-instance read = %v, %v; instances did not share one collector domain", read, err)
	}

	var caller *wago.Instance
	host := wago.HostFunc(func(callback wago.HostModule, _ []uint64, results []uint64) {
		out, callErr := target.InvokeFromHost(context.Background(), callback, "read", wago.ValueGCRef(ref).Bits())
		if callErr != nil || len(out) != 1 {
			panic(wago.HostTrap{Err: fmt.Errorf("same-domain re-entry: %v, %w", out, callErr)})
		}
		results[0] = out[0]
	})
	caller, err = rt.Instantiate(context.Background(), hostCode, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.reenter": host}))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan invokeResult, 1)
	go func() {
		out, callErr := caller.Invoke("outer", wago.I32(77))
		done <- invokeResult{values: out, err: callErr}
	}()
	select {
	case result := <-done:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 42 {
			t.Fatalf("same-domain host re-entry = %v, %v", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		// Deliberately skip cleanup on this failure path: closing a Runtime whose
		// invocation is deadlocked would hide the bounded replay diagnostic.
		t.Fatal("same-domain cross-instance host re-entry deadlocked")
	}
	prepared, err := caller.PrepareFunction("outer")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		out, callErr := prepared.Invoke(wago.I32(78))
		done <- invokeResult{values: out, err: callErr}
	}()
	select {
	case result := <-done:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 42 {
			t.Fatalf("prepared same-domain host re-entry = %v, %v", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("prepared same-domain cross-instance host re-entry deadlocked")
	}
	if err := first.ReleaseGCRef(ref); err != nil {
		t.Fatal(err)
	}
	for _, in := range []*wago.Instance{caller, first, target} {
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := hostCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gcCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConcurrencySameDomainReexportedHostReentry(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	gcCode, err := rt.Compile(harnessGCModule())
	if err != nil {
		t.Fatal(err)
	}
	reexportCode, err := rt.Compile(harnessGCReexportedHostModule())
	if err != nil {
		t.Fatal(err)
	}
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	target, err := rt.Instantiate(context.Background(), gcCode, wago.WithGC(gcCfg))
	if err != nil {
		t.Fatal(err)
	}
	var refBits uint64
	host := wago.HostFunc(func(callback wago.HostModule, _ []uint64, results []uint64) {
		out, callErr := target.InvokeFromHost(context.Background(), callback, "read", refBits)
		if callErr != nil || len(out) != 1 {
			panic(wago.HostTrap{Err: fmt.Errorf("same-domain re-exported host re-entry: %v, %w", out, callErr)})
		}
		results[0] = out[0]
	})
	reexport, err := rt.Instantiate(context.Background(), reexportCode, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.reenter": host}))
	if err != nil {
		t.Fatal(err)
	}
	created, err := reexport.Call(context.Background(), "new")
	if err != nil || len(created) != 1 || created[0].GCRef().IsNull() {
		t.Fatalf("re-exporter new = %v, %v", created, err)
	}
	ref := created[0].GCRef()
	refBits = wago.ValueGCRef(ref).Bits()
	read, err := target.Call(context.Background(), "read", wago.ValueGCRef(ref))
	if err != nil || len(read) != 1 || read[0].I32() != 42 {
		t.Fatalf("cross-instance read = %v, %v; re-exporter did not join target collector domain", read, err)
	}

	done := make(chan invokeResult, 1)
	go func() {
		out, callErr := reexport.Invoke("reenter", wago.I32(77))
		done <- invokeResult{values: out, err: callErr}
	}()
	select {
	case result := <-done:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 42 {
			t.Fatalf("same-domain re-exported host re-entry = %v, %v", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("same-domain re-exported host re-entry deadlocked")
	}
	if err := reexport.ReleaseGCRef(ref); err != nil {
		t.Fatal(err)
	}
	for _, in := range []*wago.Instance{reexport, target} {
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := reexportCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gcCode.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConcurrencyCollectGCWithInvoke(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	compiled, err := rt.Compile(harnessGCPreparedModule())
	if err != nil {
		t.Fatal(err)
	}
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	in, err := rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg))
	if err != nil {
		t.Fatal(err)
	}

	const iterations = 512
	start := make(chan struct{})
	invokeDone := make(chan error, 1)
	collectDone := make(chan error, 1)
	go func() {
		<-start
		for i := 0; i < iterations; i++ {
			if _, callErr := in.Invoke("churn", wago.I32(int32(i))); callErr != nil {
				invokeDone <- fmt.Errorf("invoke %d: %w", i, callErr)
				return
			}
		}
		invokeDone <- nil
	}()
	go func() {
		<-start
		for i := 0; i < iterations; i++ {
			if collectErr := in.CollectGC(); collectErr != nil {
				collectDone <- fmt.Errorf("collect %d: %w", i, collectErr)
				return
			}
		}
		collectDone <- nil
	}()
	close(start)
	for name, done := range map[string]<-chan error{"invoke": invokeDone, "collect": collectDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(harnessTimeout):
			t.Fatalf("concurrent %s deadlocked", name)
		}
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConcurrencyPreparedGCResultOwnership(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	compiled, err := rt.Compile(harnessGCPreparedModule())
	if err != nil {
		t.Fatal(err)
	}
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	producer, err := rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg))
	if err != nil {
		t.Fatal(err)
	}
	competitor, err := rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg))
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := producer.PrepareFunction("new")
	if err != nil {
		t.Fatal(err)
	}

	const iterations = 512
	start := make(chan struct{})
	churnDone := make(chan error, 1)
	go func() {
		<-start
		for i := 0; i < iterations*2; i++ {
			if _, callErr := competitor.Invoke("churn", wago.I32(int32(i))); callErr != nil {
				churnDone <- fmt.Errorf("churn %d: %w", i, callErr)
				return
			}
		}
		churnDone <- nil
	}()
	close(start)
	for i := 0; i < iterations; i++ {
		out, callErr := prepared.Invoke()
		if callErr != nil || len(out) != 1 || out[0] == 0 {
			t.Fatalf("prepared new %d = %v, %v", i, out, callErr)
		}
		ref := wago.ValueOf(wago.ValAnyRef, out[0]).GCRef()
		read, readErr := producer.Call(context.Background(), "read", wago.ValueGCRef(ref))
		if readErr != nil || len(read) != 1 || read[0].I32() != 42 {
			t.Fatalf("prepared result %d read = %v, %v", i, read, readErr)
		}
		if err := producer.ReleaseGCRef(ref); err != nil {
			t.Fatalf("release prepared result %d: %v", i, err)
		}
	}
	select {
	case err := <-churnDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("prepared GC result competitor deadlocked")
	}
	for _, in := range []*wago.Instance{producer, competitor} {
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeConcurrencyParkedSameDomainTargetDoesNotDeadlock(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	compiled, err := rt.Compile(harnessGCCallerModule())
	if err != nil {
		t.Fatal(err)
	}
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	parked := make(chan struct{})
	release := make(chan struct{})
	secondEntered := make(chan struct{})
	var first *wago.Instance
	host := wago.HostFunc(func(callback wago.HostModule, params, results []uint64) {
		switch id := wago.AsI32(params[0]); id {
		case 1:
			close(parked)
			<-release
			results[0] = params[0]
		case 2:
			close(secondEntered)
			out, callErr := first.InvokeFromHost(context.Background(), callback, "inner", wago.I32(42))
			if callErr != nil || len(out) != 1 {
				panic(wago.HostTrap{Err: fmt.Errorf("parked same-domain target re-entry: %v, %w", out, callErr)})
			}
			results[0] = out[0]
		default:
			panic(wago.HostTrap{Err: fmt.Errorf("unexpected host callback id %d", id)})
		}
	})
	first, err = rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.reenter": host}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg), wago.WithImports(wago.Imports{"env.reenter": host}))
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		_, callErr := first.Invoke("outer", wago.I32(1))
		// Invoke results are instance-backed and may be overwritten by the queued
		// re-entry as soon as this call releases the instance gate. Only the error
		// is an observable invariant for the parked call.
		firstDone <- callErr
	}()
	select {
	case <-parked:
	case <-time.After(harnessTimeout):
		t.Fatal("first same-domain callback did not park")
	}
	secondDone := make(chan invokeResult, 1)
	go func() {
		out, callErr := second.Invoke("outer", wago.I32(2))
		secondDone <- invokeResult{values: append([]uint64(nil), out...), err: callErr}
	}()
	select {
	case <-secondEntered:
	case <-time.After(harnessTimeout):
		t.Fatal("second same-domain callback did not attempt target re-entry")
	}
	close(release)
	select {
	case callErr := <-firstDone:
		if callErr != nil {
			t.Fatalf("first same-domain invocation: %v", callErr)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("first same-domain invocation deadlocked")
	}
	select {
	case result := <-secondDone:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 42 {
			t.Fatalf("second same-domain invocation = %v, %v; want 42", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("second same-domain invocation deadlocked")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
}

type concurrencyHarness struct {
	t      *testing.T
	seed   int64
	rounds int
	rng    *rand.Rand

	traceMu sync.Mutex
	trace   []string
}

func newHarness(t *testing.T, seed int64, rounds int) *concurrencyHarness {
	t.Helper()
	h := &concurrencyHarness{t: t, seed: seed, rounds: rounds, rng: rand.New(rand.NewSource(seed))}
	h.record("start goos=%s goarch=%s rounds=%d", goruntime.GOOS, goruntime.GOARCH, rounds)
	return h
}

func (h *concurrencyHarness) reportFailure() {
	if !h.t.Failed() {
		return
	}
	h.t.Logf("reproduce with: WAGO_CONCURRENCY_SEED=%d WAGO_CONCURRENCY_ROUNDS=%d go test -count=1 -run '^TestRuntimeConcurrencyHarness/seed-%d$' ./tests/runtimeconcurrency", h.seed, h.rounds, h.seed)
	h.traceMu.Lock()
	defer h.traceMu.Unlock()
	start := 0
	if len(h.trace) > 128 {
		start = len(h.trace) - 128
	}
	h.t.Log("bounded concurrency trace:")
	for _, event := range h.trace[start:] {
		h.t.Log(event)
	}
}

func (h *concurrencyHarness) record(format string, args ...any) {
	h.traceMu.Lock()
	if len(h.trace) < 1024 {
		h.trace = append(h.trace, fmt.Sprintf(format, args...))
	}
	h.traceMu.Unlock()
}

func (h *concurrencyHarness) deadlineContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), harnessTimeout)
}

func (h *concurrencyHarness) testThreads(t *testing.T) {
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV2 | wago.CoreFeatureThreads).WithBoundsChecks(wago.BoundsChecksExplicit)
	compiled, err := wago.Compile(cfg, harnessThreadsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	memory, err := wago.NewSharedMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()

	workers := 3 + h.rng.Intn(3)
	instances := make([]*wago.Instance, workers)
	for i := range instances {
		instances[i], err = wago.Instantiate(compiled, wago.Imports{"env.memory": memory})
		if err != nil {
			t.Fatalf("instantiate worker %d: %v", i, err)
		}
	}
	defer func() {
		for _, in := range instances {
			if in != nil {
				_ = in.Close()
			}
		}
	}()

	if err := memory.Close(); err == nil {
		t.Fatal("shared memory closed with live importers")
	}

	plans := make([][]int32, workers)
	var want uint32
	for worker := range workers {
		plans[worker] = make([]int32, h.rounds)
		for round := range h.rounds {
			delta := int32(1 + h.rng.Intn(5))
			plans[worker][round] = delta
			want += uint32(delta)
		}
	}
	h.record("threads workers=%d atomic-total=%d", workers, want)

	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for worker, in := range instances {
		worker, in := worker, in
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for round, delta := range plans[worker] {
				out, callErr := in.Invoke("add", wago.I32(delta))
				if callErr != nil || len(out) != 1 {
					errCh <- fmt.Errorf("worker %d round %d add(%d) = %v, %v", worker, round, delta, out, callErr)
					return
				}
			}
		}()
	}
	close(start)
	waitForWorkers(t, &wg, "thread atomic-add")
	close(errCh)
	for callErr := range errCh {
		t.Error(callErr)
	}
	if t.Failed() {
		return
	}
	if got := binary.LittleEndian.Uint32(memory.UnsafeBytes()[:4]); got != want {
		t.Fatalf("shared atomic total = %d, want %d", got, want)
	}

	marker := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
	atomic.StoreUint32(marker, 0)
	ctx, cancel := h.deadlineContext()
	defer cancel()
	waitDone := make(chan invokeResult, 1)
	go func() {
		out, callErr := instances[0].InvokeContext(ctx, "wait-marked")
		waitDone <- invokeResult{values: out, err: callErr}
	}()
	waitForMarker(t, marker)
	for {
		out, callErr := instances[1].Invoke("notify", wago.I32(8), wago.I32(1))
		if callErr != nil || len(out) != 1 {
			t.Fatalf("notify = %v, %v", out, callErr)
		}
		if wago.AsI32(out[0]) == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("waiter never became observable to atomic.notify")
		default:
			goruntime.Gosched()
		}
	}
	select {
	case result := <-waitDone:
		if result.err != nil || len(result.values) != 1 || wago.AsI32(result.values[0]) != 0 {
			t.Fatalf("notified wait = %v, %v; want [0], nil", result.values, result.err)
		}
	case <-ctx.Done():
		t.Fatal("notified waiter did not resume")
	}

	atomic.StoreUint32(marker, 0)
	closeWaitDone := make(chan invokeResult, 1)
	go func() {
		out, callErr := instances[0].Invoke("wait-marked")
		closeWaitDone <- invokeResult{values: out, err: callErr}
	}()
	waitForMarker(t, marker)
	if err := instances[0].Close(); err != nil {
		t.Fatalf("close waiting instance: %v", err)
	}
	select {
	case result := <-closeWaitDone:
		var trap *wago.TrapError
		if !errors.As(result.err, &trap) || trap.Code != wago.TrapInterrupted {
			t.Fatalf("close-interrupted wait = %v, %v", result.values, result.err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("close did not release waiting invocation")
	}
	if _, err := instances[0].Invoke("add", wago.I32(1)); err == nil {
		t.Fatal("closed threaded instance accepted another invocation")
	}
	instances[0] = nil

	if err := memory.Close(); err == nil {
		t.Fatal("shared memory closed while other importers remained live")
	}
	order := h.rng.Perm(workers - 1)
	for _, index := range order {
		actual := index + 1
		if err := instances[actual].Close(); err != nil {
			t.Fatalf("close worker %d: %v", actual, err)
		}
		instances[actual] = nil
	}
	if err := memory.Close(); err != nil {
		t.Fatalf("close shared memory after consumers: %v", err)
	}
	if memory.UnsafeBytes() != nil {
		t.Fatal("closed shared memory retained a host-visible byte view")
	}
	h.record("threads complete close-order=%v", order)
}

func (h *concurrencyHarness) testHostReentry(t *testing.T) {
	compiled, err := wago.Compile(nil, harnessHostReentryModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	workers := 2 + h.rng.Intn(3)
	instances := make([]*wago.Instance, workers)
	var hostCalls atomic.Uint64
	for worker := range workers {
		var in *wago.Instance
		host := wago.HostFunc(func(caller wago.HostModule, params, results []uint64) {
			hostCalls.Add(1)
			goruntime.GC()
			out, callErr := in.InvokeFromHost(context.Background(), caller, "inner", params[0])
			if callErr != nil || len(out) != 1 {
				panic(wago.HostTrap{Err: fmt.Errorf("nested inner: %v, %w", out, callErr)})
			}
			results[0] = out[0]
		})
		in, err = wago.Instantiate(compiled, wago.InstantiateOptions{Imports: wago.Imports{"env.reenter": host}})
		if err != nil {
			t.Fatalf("instantiate reentry worker %d: %v", worker, err)
		}
		instances[worker] = in
	}
	defer func() {
		for _, in := range instances {
			if in != nil {
				_ = in.Close()
			}
		}
	}()

	plans := make([][]int32, workers)
	for worker := range workers {
		plans[worker] = make([]int32, h.rounds)
		for round := range h.rounds {
			plans[worker][round] = int32(h.rng.Intn(10_000))
		}
	}
	start := make(chan struct{})
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for worker, in := range instances {
		worker, in := worker, in
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for round, input := range plans[worker] {
				out, callErr := in.Invoke("outer", wago.I32(input))
				if callErr != nil || len(out) != 1 || wago.AsI32(out[0]) != input+3 {
					errCh <- fmt.Errorf("worker %d round %d outer(%d) = %v, %v", worker, round, input, out, callErr)
					return
				}
			}
		}()
	}
	close(start)
	waitForWorkers(t, &wg, "host re-entry")
	close(errCh)
	for callErr := range errCh {
		t.Error(callErr)
	}
	if t.Failed() {
		return
	}
	if want := uint64(workers * h.rounds); hostCalls.Load() != want {
		t.Fatalf("host callback count = %d, want %d", hostCalls.Load(), want)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	nested := make(chan error, 1)
	var parked *wago.Instance
	parkedHost := wago.HostFunc(func(caller wago.HostModule, params, results []uint64) {
		close(entered)
		<-release
		out, callErr := parked.InvokeFromHost(context.Background(), caller, "inner", params[0])
		if callErr == nil {
			if len(out) != 1 {
				callErr = fmt.Errorf("inner returned %v", out)
			} else {
				results[0] = out[0]
			}
		}
		nested <- callErr
	})
	parked, err = wago.Instantiate(compiled, wago.InstantiateOptions{Imports: wago.Imports{"env.reenter": parkedHost}})
	if err != nil {
		t.Fatal(err)
	}
	invokeDone := make(chan invokeResult, 1)
	go func() {
		out, callErr := parked.Invoke("outer", wago.I32(9))
		invokeDone <- invokeResult{values: out, err: callErr}
	}()
	select {
	case <-entered:
	case <-time.After(harnessTimeout):
		t.Fatal("host callback did not park")
	}
	closeStarted := make(chan struct{})
	closeDone := make(chan error, 1)
	go func() {
		close(closeStarted)
		closeDone <- parked.Close()
	}()
	<-closeStarted
	goruntime.Gosched()
	close(release)

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("concurrent parked close: %v", err)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("concurrent parked close timed out")
	}
	select {
	case result := <-invokeDone:
		if result.err == nil && (len(result.values) != 1 || wago.AsI32(result.values[0]) != 12) {
			t.Fatalf("parked invocation completed with corrupt result %v", result.values)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("parked invocation did not unwind")
	}
	select {
	case nestedErr := <-nested:
		if nestedErr == nil {
			h.record("host close allowed already-authorized nested reentry to finish")
		} else {
			h.record("host close rejected nested reentry: %v", nestedErr)
		}
	case <-time.After(harnessTimeout):
		t.Fatal("parked host callback did not finish")
	}
	if _, err := parked.Invoke("outer", wago.I32(1)); err == nil {
		t.Fatal("closed host-reentry instance accepted another invocation")
	}
	h.record("host reentry workers=%d callbacks=%d", workers, hostCalls.Load())
}

func (h *concurrencyHarness) testGC(t *testing.T) {
	if !completeCore3Host() {
		t.Skipf("complete Core 3 GC execution is unavailable on %s/%s", goruntime.GOOS, goruntime.GOARCH)
	}
	cfg := wago.NewRuntimeConfig().WithCoreFeatures(wago.CoreFeaturesV3).WithBoundsChecks(wago.BoundsChecksExplicit)
	rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
	defer rt.Close()
	compiled, err := rt.Compile(harnessGCModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	workers := 2 + h.rng.Intn(3)
	instances := make([]*wago.Instance, workers)
	gcCfg := wago.GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}
	for worker := range workers {
		instances[worker], err = rt.Instantiate(context.Background(), compiled, wago.WithGC(gcCfg))
		if err != nil {
			t.Fatalf("instantiate GC worker %d: %v", worker, err)
		}
	}
	defer func() {
		for _, in := range instances {
			if in != nil {
				_ = in.Close()
			}
		}
	}()

	held := make([]wago.GCRef, workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for worker, in := range instances {
		worker, in := worker, in
		wg.Add(1)
		go func() {
			defer wg.Done()
			values, callErr := in.Call(context.Background(), "new")
			if callErr != nil || len(values) != 1 || values[0].GCRef().IsNull() {
				errCh <- fmt.Errorf("worker %d create held GC object = %v, %v", worker, values, callErr)
				return
			}
			held[worker] = values[0].GCRef()
		}()
	}
	waitForWorkers(t, &wg, "GC retained-object creation")
	close(errCh)
	for callErr := range errCh {
		t.Error(callErr)
	}
	if t.Failed() {
		return
	}

	churn := make([]int, workers)
	for worker := range workers {
		churn[worker] = h.rounds * (1 + h.rng.Intn(3))
	}
	errCh = make(chan error, workers)
	for worker, in := range instances {
		worker, in := worker, in
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < churn[worker]; iteration++ {
				values, callErr := in.Call(context.Background(), "new")
				if callErr != nil || len(values) != 1 || values[0].GCRef().IsNull() {
					errCh <- fmt.Errorf("worker %d churn %d create = %v, %v", worker, iteration, values, callErr)
					return
				}
				if releaseErr := in.ReleaseGCRef(values[0].GCRef()); releaseErr != nil {
					errCh <- fmt.Errorf("worker %d churn %d release: %w", worker, iteration, releaseErr)
					return
				}
			}
		}()
	}
	waitForWorkers(t, &wg, "GC allocation churn")
	close(errCh)
	for callErr := range errCh {
		t.Error(callErr)
	}
	if t.Failed() {
		return
	}

	errCh = make(chan error, workers)
	for worker, in := range instances {
		worker, in := worker, in
		wg.Add(1)
		go func() {
			defer wg.Done()
			values, callErr := in.Call(context.Background(), "read", wago.ValueGCRef(held[worker]))
			if callErr != nil || len(values) != 1 || values[0].I32() != 42 {
				errCh <- fmt.Errorf("worker %d read held GC object = %v, %v", worker, values, callErr)
			}
		}()
	}
	waitForWorkers(t, &wg, "GC retained-object reads")
	close(errCh)
	for callErr := range errCh {
		t.Error(callErr)
	}
	if t.Failed() {
		return
	}

	order := h.rng.Perm(workers)
	errCh = make(chan error, workers)
	for _, worker := range order {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			if closeErr := instances[worker].Close(); closeErr != nil {
				errCh <- fmt.Errorf("close GC worker %d: %w", worker, closeErr)
			}
		}()
	}
	waitForWorkers(t, &wg, "GC instance close")
	close(errCh)
	for closeErr := range errCh {
		t.Error(closeErr)
	}
	if t.Failed() {
		return
	}

	errCh = make(chan error, workers)
	for worker, in := range instances {
		worker, in := worker, in
		wg.Add(1)
		go func() {
			defer wg.Done()
			if releaseErr := in.ReleaseGCRef(held[worker]); releaseErr != nil {
				errCh <- fmt.Errorf("release worker %d token after close: %w", worker, releaseErr)
			}
		}()
	}
	waitForWorkers(t, &wg, "GC token release")
	close(errCh)
	for releaseErr := range errCh {
		t.Error(releaseErr)
	}
	if t.Failed() {
		return
	}
	for i := range instances {
		instances[i] = nil
	}
	h.record("gc workers=%d churn=%v close-order=%v", workers, churn, order)
}

type invokeResult struct {
	values []uint64
	err    error
}

func waitForWorkers(t *testing.T, wg *sync.WaitGroup, phase string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(harnessTimeout):
		t.Fatalf("%s workers did not complete within %s", phase, harnessTimeout)
	}
}

func waitForMarker(t *testing.T, marker *uint32) {
	t.Helper()
	deadline := time.Now().Add(harnessTimeout)
	for atomic.LoadUint32(marker) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("guest did not publish its wait marker")
		}
		goruntime.Gosched()
	}
}

func harnessSeeds(t *testing.T) []int64 {
	t.Helper()
	if path := os.Getenv("WAGO_CONCURRENCY_SEED_FILE"); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read WAGO_CONCURRENCY_SEED_FILE: %v", err)
		}
		return parseSeeds(t, string(contents))
	}
	if raw := os.Getenv("WAGO_CONCURRENCY_SEED"); raw != "" {
		return parseSeeds(t, raw)
	}
	return []int64{439_000_001, 439_000_019}
}

func parseSeeds(t *testing.T, raw string) []int64 {
	t.Helper()
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' })
	seeds := make([]int64, 0, len(fields))
	for _, field := range fields {
		if strings.HasPrefix(field, "#") {
			continue
		}
		seed, err := strconv.ParseInt(field, 0, 64)
		if err != nil {
			t.Fatalf("parse concurrency seed %q: %v", field, err)
		}
		seeds = append(seeds, seed)
	}
	if len(seeds) == 0 {
		t.Fatal("concurrency seed input is empty")
	}
	return seeds
}

func harnessRounds(t *testing.T) int {
	t.Helper()
	rounds := defaultHarnessRounds
	if testing.Short() {
		rounds = 4
	}
	if raw := os.Getenv("WAGO_CONCURRENCY_ROUNDS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1_000 {
			t.Fatalf("WAGO_CONCURRENCY_ROUNDS=%q must be an integer in [1,1000]", raw)
		}
		rounds = parsed
	}
	return rounds
}

func completeCore3Host() bool {
	return (goruntime.GOOS == "linux" && (goruntime.GOARCH == "amd64" || goruntime.GOARCH == "arm64")) || (goruntime.GOOS == "darwin" && goruntime.GOARCH == "arm64")
}

func harnessGCHostProducerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	voidSig := wasmtest.FuncType(nil, nil)
	i32Sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("collect")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, voidSig, i32Sig)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("call", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("nested", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x10, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}),
		)),
	)
}

func harnessGCCrossInstanceConsumerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	i32Sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("call")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, i32Sig)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x10, 0x00, 0x0b}),
		)),
	)
}

func harnessGCThreadsModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structType,
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("add", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("notify", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("wait-marked", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("allocate", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x00, 0x20, 0x00, 0xfe, 0x1e, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0xfe, 0x00, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{
				0x41, 0x04, 0x41, 0x01, 0xfe, 0x17, 0x02, 0x00,
				0x41, 0x08, 0x41, 0x00, 0x42, 0x7f, 0xfe, 0x01, 0x02, 0x00,
				0x0b,
			}),
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x20, 0x00, 0x0b}),
		)),
	)
}

func harnessThreadsModule() []byte {
	memoryImport := append(wasmtest.Name("env"), wasmtest.Name("memory")...)
	memoryImport = append(memoryImport, 0x02, 0x03, 0x01, 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("add", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("notify", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("wait-marked", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x00, 0x20, 0x00, 0xfe, 0x1e, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0xfe, 0x00, 0x02, 0x00, 0x0b}),
			wasmtest.Code([]byte{
				0x41, 0x04, 0x41, 0x01, 0xfe, 0x17, 0x02, 0x00,
				0x41, 0x08, 0x41, 0x00, 0x42, 0x7f, 0xfe, 0x01, 0x02, 0x00,
				0x0b,
			}),
		)),
	)
}

func harnessHostReentryModule() []byte {
	i32 := []wasm.ValType{wasm.I32}
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("reenter")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x00)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, i32))),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("inner", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("outer", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x41, 0x02, 0x6a, 0x0b}),
		)),
	)
}

func harnessGCReexportedHostModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	newSig := []byte{0x60, 0x00, 0x01, 0x64, 0x00}
	readSig := []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}
	i32Sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("reenter")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x03)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, newSig, readSig, i32Sig)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("reenter", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("new", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}),
		)),
	)
}

func harnessGCCallerModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	i32Sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("reenter")...)
	importEntry = append(importEntry, byte(wasm.ExternFunc), 0x01)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, i32Sig)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("inner", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("outer", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x00, 0x0b}),
		)),
	)
}

func harnessGCPreparedModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	newSig := []byte{0x60, 0x00, 0x01, 0x64, 0x00}
	readSig := []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}
	i32Sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, newSig, readSig, i32Sig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("churn", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x20, 0x00, 0x0b}),
		)),
	)
}

func harnessGCModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	newSig := []byte{0x60, 0x00, 0x01, 0x64, 0x00}
	readSig := []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f}
	i32Sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, newSig, readSig, i32Sig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}),
		)),
	)
}
