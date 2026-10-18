package wago

import (
	"context"
	"errors"
	"sync"
	"testing"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func cleanupMappingFixture(t *testing.T) (*Compiled, *compiledCacheIndexes, map[uintptr]int) {
	t.Helper()
	c := newCompilerCompiled(Compiled{})
	indexes := c.ensureCompileIndexes()
	calls := make(map[uintptr]int)
	indexes.unmapCode = func(mem []byte) error {
		ptr := uintptr(unsafe.Pointer(&mem[0]))
		calls[ptr]++
		if calls[ptr] == 1 {
			return errors.New("injected unmap failure")
		}
		return coreruntime.Unmap(mem)
	}
	t.Cleanup(func() {
		indexes.unmapCode = nil
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c, indexes, calls
}

func cleanupMapping(t *testing.T) []byte {
	t.Helper()
	b, err := coreruntime.NewCodeBuffer(1)
	if err != nil {
		t.Fatal(err)
	}
	mem, _, err := b.Take()
	if err != nil {
		_ = b.Close()
		t.Fatal(err)
	}
	return mem
}

func TestCompiledCloseRetriesEachFailedMapping(t *testing.T) {
	c, indexes, calls := cleanupMappingFixture(t)
	main, shared, private := cleanupMapping(t), cleanupMapping(t), cleanupMapping(t)
	c.codeCache.mem, c.codeCache.base = main, uintptr(unsafe.Pointer(&main[0]))
	c.validateMemo.hostThunks[0] = compiledHostThunkCache{mem: shared, base: uintptr(unsafe.Pointer(&shared[0])), offsets: []int{0}}
	c.retainUnpublishedMapping(private)
	if err := c.Close(); err == nil {
		t.Fatal("failed cleanup was hidden")
	}
	if c.codeCache.mem == nil || c.codeCache.base != 0 || c.validateMemo.hostThunks[0].mem == nil || c.validateMemo.hostThunks[0].base != 0 || len(indexes.failedMappings) != 1 {
		t.Fatal("close lost ownership or left callable publication state")
	}
	if _, err := c.acquireCode(); err == nil {
		t.Fatal("closed owner permitted execution")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.codeCache.mem != nil || c.validateMemo.hostThunks[0].mem != nil || len(indexes.failedMappings) != 0 {
		t.Fatal("successful retry retained mappings")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for _, mem := range [][]byte{main, shared, private} {
		if calls[uintptr(unsafe.Pointer(&mem[0]))] != 2 {
			t.Fatal("mapping released more than once")
		}
	}
}

func TestCompiledReleaseAfterCloseRetainsFailedMappings(t *testing.T) {
	c, _, _ := cleanupMappingFixture(t)
	c.codeCache.mem, c.codeCache.refs = cleanupMapping(t), 1
	c.validateMemo.hostThunks[0].mem = cleanupMapping(t)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c.releaseCode()
	if c.codeCache.mem == nil || c.validateMemo.hostThunks[0].mem == nil {
		t.Fatal("deferred failed release dropped ownership")
	}
	if err := c.Close(); err != nil || c.codeCache.mem != nil || c.validateMemo.hostThunks[0].mem != nil {
		t.Fatal("deferred cleanup retry", err)
	}
}

func TestCompiledFailedPrivateReleaseTransfersOwnership(t *testing.T) {
	c, indexes, _ := cleanupMappingFixture(t)
	if err := c.releaseOwnedCodeMapping(cleanupMapping(t)); err == nil || len(indexes.failedMappings) != 1 {
		t.Fatal("private failed release dropped ownership", err)
	}
	if err := c.Close(); err != nil || len(indexes.failedMappings) != 0 {
		t.Fatal("private cleanup retry", err)
	}
}

func TestCompiledReplaceFailurePreservesOldOwner(t *testing.T) {
	c, _, _ := cleanupMappingFixture(t)
	c.codeCache.mem = cleanupMapping(t)
	c.validateMemo.hostThunks[0].mem = cleanupMapping(t)
	if err := c.replaceDecoded(Compiled{}, 0); err == nil {
		t.Fatal("replacement ignored failed release")
	}
	if !c.codeCache.closed || c.codeCache.mem == nil || c.validateMemo.hostThunks[0].mem == nil {
		t.Fatal("replacement lost failed cleanup owner")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestThunkPublicationFailuresKeepOwners(t *testing.T) {
	for _, private := range []bool{false, true} {
		name := "shared"
		if private {
			name = "private"
		}
		t.Run(name, func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			mod, err := rt.Compile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			c := mod.c
			indexes := c.ensureCompileIndexes()
			failure := errors.New("injected thunk publication/rollback failure")
			var retained []byte
			publications := 0
			indexes.mapThunkCode = func([]byte) ([]byte, uintptr, error) {
				publications++
				retained = cleanupMapping(t)
				return retained, 0, failure
			}
			indexes.unmapCode = func(mem []byte) error {
				if len(retained) != 0 && uintptr(unsafe.Pointer(&mem[0])) == uintptr(unsafe.Pointer(&retained[0])) {
					return failure
				}
				return coreruntime.Unmap(mem)
			}
			defer func() {
				indexes.mapThunkCode, indexes.unmapCode = nil, nil
			}()
			var binding any = slotHostFunc(func(_ HostModule, _, _ []uint64) { t.Error("failed publication invoked host") })
			if private {
				binding, err = rt.NewHostFuncRef(binding, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
				if err != nil {
					t.Fatal(err)
				}
			}
			instance, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", binding)))
			if instance != nil || !errors.Is(err, failure) || publications != 1 {
				t.Fatal("failed thunk published", instance, err, publications)
			}
			if private {
				if len(indexes.failedMappings) != 1 {
					t.Fatal("private failure owner lost")
				}
			} else if cache := c.validateMemo.hostThunks[1]; cache.mem == nil || cache.base != 0 || cache.offsets != nil {
				t.Fatal("shared failure owner lost or callable")
			}
			// A persistent failure must block another publication rather than
			// accumulate another failed mapping.
			instance, err = rt.Instantiate(context.Background(), mod, WithImports(testImports("env.f", binding)))
			if instance != nil || err == nil || publications != 1 {
				t.Fatal("pending cleanup permitted another publication", instance, err, publications)
			}
			if err := c.Close(); !errors.Is(err, failure) {
				t.Fatal("close dropped thunk failure", err)
			}
			indexes.unmapCode = nil
			if err := c.Close(); err != nil || len(indexes.failedMappings) != 0 || c.validateMemo.hostThunks[1].mem != nil {
				t.Fatal("thunk cleanup retry", err)
			}
		})
	}
}

func TestThunkCleanupGateCoversBothCallModes(t *testing.T) {
	for failedMode := range 2 {
		c := MustCompile(returningImportModule(returningI32Sig(), []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}))
		indexes := c.ensureCompileIndexes()
		mem := cleanupMapping(t)
		c.validateMemo.hostThunks[failedMode].mem = mem
		failure := errors.New("persistent shared rollback failure")
		indexes.unmapCode = func(mapping []byte) error {
			if uintptr(unsafe.Pointer(&mapping[0])) == uintptr(unsafe.Pointer(&mem[0])) {
				return failure
			}
			return coreruntime.Unmap(mapping)
		}
		indexes.mapThunkCode = func([]byte) ([]byte, uintptr, error) {
			t.Error("pending shared failure permitted a new publication")
			return nil, 0, failure
		}
		if base, _, err := c.sharedHostFuncThunks(failedMode == 0); base != 0 || !errors.Is(err, failure) {
			t.Fatal("other shared call mode bypassed cleanup gate", base, err)
		}
		if mapping, base, err := c.mapPrivateThunkCode([]byte{1}); mapping != nil || base != 0 || !errors.Is(err, failure) {
			t.Fatal("private call mode bypassed shared cleanup gate", mapping, base, err)
		}
		indexes.mapThunkCode, indexes.unmapCode = nil, nil
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConcurrentPrivatePublicationRetainsFailureBeforeNextBuilder(t *testing.T) {
	c, indexes, _ := cleanupMappingFixture(t)
	failure := errors.New("persistent concurrent rollback failure")
	mem := cleanupMapping(t)
	indexes.unmapCode = func([]byte) error { return failure }
	firstMapped, releaseFirst, secondStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	publications := 0
	indexes.mapThunkCode = func([]byte) ([]byte, uintptr, error) {
		publications++
		if publications == 1 {
			close(firstMapped)
			<-releaseFirst
		}
		return mem, 0, failure
	}
	var builders sync.WaitGroup
	builders.Add(2)
	go func() {
		defer builders.Done()
		if mapping, entry, err := c.mapPrivateThunkCode([]byte{1}); mapping != nil || entry != 0 || !errors.Is(err, failure) {
			t.Error("first failed mapping escaped owner", mapping, entry, err)
		}
	}()
	<-firstMapped
	go func() {
		defer builders.Done()
		close(secondStarted)
		if mapping, entry, err := c.mapPrivateThunkCode([]byte{2}); mapping != nil || entry != 0 || !errors.Is(err, failure) {
			t.Error("second builder bypassed cleanup", mapping, entry, err)
		}
	}()
	<-secondStarted
	close(releaseFirst)
	builders.Wait()
	if publications != 1 || len(indexes.failedMappings) != 1 {
		t.Fatal("concurrent failure multiplied publication owners", publications, len(indexes.failedMappings))
	}
	indexes.mapThunkCode, indexes.unmapCode = nil, nil
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCompiledSealFailureClosesOwnerAfterRX(t *testing.T) {
	c := MustCompile(benchAddOneModule())
	indexes := c.ensureCompileIndexes()
	failure := errors.New("injected failure after RX publication")
	seals, unmaps := 0, 0
	indexes.sealCode = func(mem []byte) error {
		seals++
		if err := coreruntime.SealCode(mem); err != nil {
			return err
		}
		return failure
	}
	indexes.unmapCode = func(mem []byte) error {
		unmaps++
		if unmaps == 1 {
			return failure
		}
		return coreruntime.Unmap(mem)
	}
	t.Cleanup(func() {
		indexes.sealCode, indexes.unmapCode = nil, nil
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	if entry, err := c.acquireCode(); entry != 0 || !errors.Is(err, failure) {
		t.Fatal("failed seal published entry", entry, err)
	}
	if !c.codeCache.closed || c.codeCache.mem == nil || c.codeCache.base != 0 || c.code != nil || c.validateMemo.executionView().code != nil {
		t.Fatal("failed RX seal reopened module or lost mapping")
	}
	if entry, err := c.acquireCode(); entry != 0 || err == nil || seals != 1 {
		t.Fatal("failed RX owner attempted another publication", entry, err, seals)
	}
	if err := c.Close(); !errors.Is(err, failure) || c.codeCache.mem == nil {
		t.Fatal("failed cleanup lost RX mapping", err)
	}
	if err := c.Close(); err != nil || c.codeCache.mem != nil || unmaps != 2 {
		t.Fatal("failed RX cleanup retry", err, unmaps)
	}
}

func TestInstantiateSealFailureClearsPublicAndExecutionViews(t *testing.T) {
	rt := NewRuntime()
	defer rt.Close()
	mod, err := rt.Compile(benchAddOneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	public := mod.Compiled()
	if public == mod.c {
		t.Fatal("fixture must have distinct public and execution views")
	}
	indexes := public.ensureCompileIndexes()
	failure := errors.New("injected instantiated RX failure")
	indexes.sealCode = func(mem []byte) error {
		if err := coreruntime.SealCode(mem); err != nil {
			return err
		}
		return failure
	}
	defer func() { indexes.sealCode = nil }()
	instance, err := rt.Instantiate(context.Background(), mod)
	if instance != nil || !errors.Is(err, failure) {
		t.Fatal("failed seal became callable", instance, err)
	}
	if public.code != nil || mod.c.code != nil || !public.codeCache.closed || public.codeCache.mem == nil || public.codeCache.base != 0 {
		t.Fatal("real instantiation lost ownership or left a code view")
	}
	if instance, err := rt.Instantiate(context.Background(), mod); instance != nil || err == nil {
		t.Fatal("closed RX owner admitted instantiation", instance, err)
	}
	if err := public.Close(); err != nil {
		t.Fatal(err)
	}
}
