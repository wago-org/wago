//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func stagedTagImportEntry(module, name string, typeIndex uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, byte(wasm.ExternTag), 0x00)
	return append(out, wasmtest.ULEB(typeIndex)...)
}

func stagedTagProductProducerModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00}, []byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("primary", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("alias", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("other", byte(wasm.ExternTag), 1),
		)),
	)
}

func stagedTagProductConsumerModule(firstType, secondType uint32) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.F64}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I64}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			stagedTagImportEntry("env", "first", firstType),
			stagedTagImportEntry("env", "second", secondType),
		)),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("reexport", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("local", byte(wasm.ExternTag), 2),
		)),
	)
}

func stagedFuncImportEntry(module, name string, typeIndex uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, byte(wasm.ExternFunc))
	return append(out, wasmtest.ULEB(typeIndex)...)
}

func stagedCrossInstanceEHProviderModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("tag", byte(wasm.ExternTag), 0),
			wasmtest.ExportEntry("throw", byte(wasm.ExternFunc), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0x00, 0x0b}))),
	)
}

func stagedCrossInstanceEHConsumerModule() []byte {
	catchImported := []byte{
		0x02, 0x40,
		0x1f, 0x7f, 0x01, 0x00, 0x00, 0x00,
		0x10, 0x00,
		0x41, 0x09,
		0x0b,
		0x0f,
		0x0b,
		0x41, 0x02,
		0x0b,
	}
	catchAlias := []byte{
		0x02, 0x40,
		0x1f, 0x7f, 0x01, 0x00, 0x00, 0x00,
		0x08, 0x01,
		0x41, 0x09,
		0x0b,
		0x0f,
		0x0b,
		0x41, 0x02,
		0x0b,
	}
	mismatch := []byte{
		0x02, 0x40,
		0x1f, 0x7f, 0x01, 0x02, 0x00,
		0x02, 0x40,
		0x1f, 0x7f, 0x01, 0x00, 0x02, 0x00,
		0x41, 0x01,
		0x10, 0x00,
		0x0b,
		0x0f,
		0x0b,
		0x41, 0x02,
		0x0b,
		0x0f,
		0x0b,
		0x41, 0x03,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(
			stagedTagImportEntry("env", "tag", 0),
			stagedTagImportEntry("env", "tag-alias", 0),
			stagedFuncImportEntry("env", "throw", 0),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01}, []byte{0x01}, []byte{0x01})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0x00, 0x00})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("catch-imported", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("catch-alias", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("mismatch", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(catchImported), wasmtest.Code(catchAlias), wasmtest.Code(mismatch))),
	)
}

func stagedImportedTagThrowModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(stagedTagImportEntry("env", "tag", 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x00})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x08, 0x00, 0x0b}))),
	)
}

func TestStagedTagProductMetadataIdentityLifecycle(t *testing.T) {
	producerCompiled := compileStagedExceptionHandling(t, stagedTagProductProducerModule())
	defer producerCompiled.Close()
	producerMeta := (&Module{c: producerCompiled}).Metadata()
	if len(producerMeta.Tags) != 2 || !reflect.DeepEqual(producerMeta.Tags[0].Exports, []string{"alias", "primary"}) || !reflect.DeepEqual(producerMeta.Tags[1].Exports, []string{"other"}) {
		t.Fatalf("producer tag metadata = %#v", producerMeta.Tags)
	}
	if err := applyPolicy(&Module{c: producerCompiled}, Policy{MaxTags: 2}); err != nil {
		t.Fatalf("exact tag policy: %v", err)
	}
	if err := applyPolicy(&Module{c: producerCompiled}, Policy{MaxTags: 1}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("tag policy error = %v, want ErrPermissionDenied", err)
	}
	blob, err := producerCompiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal tag product: %v", err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err != nil {
		t.Fatalf("unmarshal tag product: %v", err)
	}
	defer loaded.Close()
	if got := (&Module{c: &loaded}).Metadata().Tags; !reflect.DeepEqual(got, producerMeta.Tags) {
		t.Fatalf("reloaded tag metadata = %#v, want %#v", got, producerMeta.Tags)
	}
	loadedProducer, err := instantiateCore(&loaded, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate reloaded tag producer: %v", err)
	}
	loadedPrimary, err := loadedProducer.ExportedTag("primary")
	if err != nil {
		t.Fatal(err)
	}
	loadedAlias, err := loadedProducer.ExportedTag("alias")
	if err != nil || loadedAlias != loadedPrimary {
		t.Fatalf("reloaded duplicate alias = %p, %v; want %p", loadedAlias, err, loadedPrimary)
	}
	if err := loadedProducer.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(producerCompiled, SnapshotOptions{})
	if err != nil {
		t.Fatalf("capture declaration-only local tags: %v", err)
	}
	if _, err := snapshot.MarshalBinary(); err != nil {
		t.Fatalf("marshal declaration-only tag snapshot: %v", err)
	}

	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate tag producer: %v", err)
	}
	primary, err := producer.ExportedTag("primary")
	if err != nil {
		t.Fatal(err)
	}
	alias, err := producer.ExportedTag("alias")
	if err != nil || alias != primary {
		t.Fatalf("duplicate tag alias = %p, %v; want %p", alias, err, primary)
	}
	other, err := producer.ExportedTag("other")
	if err != nil || other == primary {
		t.Fatalf("distinct tag identity = %p, %v; primary %p", other, err, primary)
	}

	consumerCompiled := compileStagedExceptionHandling(t, stagedTagProductConsumerModule(0, 0))
	defer consumerCompiled.Close()
	consumerBlob, err := consumerCompiled.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal imported tag product: %v", err)
	}
	var loadedConsumer Compiled
	if err := loadedConsumer.UnmarshalBinary(consumerBlob); err != nil {
		t.Fatalf("unmarshal imported tag product: %v", err)
	}
	defer loadedConsumer.Close()
	loadedConsumerInstance, err := instantiateCore(&loadedConsumer, InstantiateOptions{Imports: Imports{"env.first": primary, "env.second": primary}})
	if err != nil {
		t.Fatalf("instantiate reloaded imported tags: %v", err)
	}
	loadedReexport, err := loadedConsumerInstance.ExportedTag("reexport")
	if err != nil || loadedReexport != primary {
		t.Fatalf("reloaded tag re-export = %p, %v; want %p", loadedReexport, err, primary)
	}
	if err := loadedConsumerInstance.Close(); err != nil {
		t.Fatal(err)
	}
	rt := NewRuntime()
	defer rt.Close()
	rt.imports = Imports{"env.first": primary, "env.second": primary}
	consumerModule := rt.buildModule(consumerCompiled)
	imports := consumerModule.Imports()
	if len(imports) != 2 || imports[0].Kind != ImportTag || imports[0].Index != 0 || imports[1].Index != 1 || !reflect.DeepEqual(imports[0].Params, []ValType{ValI32, ValF64}) {
		t.Fatalf("tag import inspection = %#v", imports)
	}
	if _, err := Capture(consumerCompiled, SnapshotOptions{Imports: Imports{"env.first": primary, "env.second": primary}}); err == nil || !strings.Contains(err.Error(), "declaration-only local tags") {
		t.Fatalf("imported tag snapshot = %v, want local-only rejection", err)
	}
	consumer, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{"env.first": primary, "env.second": primary}})
	if err != nil {
		t.Fatalf("instantiate tag consumer: %v", err)
	}
	reexport, err := consumer.ExportedTag("reexport")
	if err != nil || reexport != primary {
		t.Fatalf("tag re-export identity = %p, %v; want %p", reexport, err, primary)
	}
	local, err := consumer.ExportedTag("local")
	if err != nil || local == primary {
		t.Fatalf("consumer local tag identity = %p, %v; provider %p", local, err, primary)
	}
	consumerMeta := (&Module{c: consumerCompiled}).Metadata()
	if len(consumerMeta.Tags) != 3 || consumerMeta.Tags[0].ImportModule != "env" || consumerMeta.Tags[0].ImportName != "first" || consumerMeta.Tags[1].ImportName != "second" || !reflect.DeepEqual(consumerMeta.Tags[0].Exports, []string{"reexport"}) || !reflect.DeepEqual(consumerMeta.Tags[2].Exports, []string{"local"}) {
		t.Fatalf("consumer tag metadata = %#v", consumerMeta.Tags)
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("logical producer close: %v", err)
	}
	producer.lifeMu.Lock()
	refs, resourcesClosed := producer.resourceRefs, producer.resourcesClosed
	producer.lifeMu.Unlock()
	if refs != 1 || resourcesClosed {
		t.Fatalf("duplicate tag imports retained refs=%d resourcesClosed=%v, want 1/false", refs, resourcesClosed)
	}
	if err := consumer.Close(); err != nil {
		t.Fatalf("consumer close: %v", err)
	}
	producer.lifeMu.Lock()
	resourcesClosed = producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !resourcesClosed {
		t.Fatal("producer resources remained live after final tag consumer close")
	}
}

func TestStagedTagProductRollbackAndImportedThrowCompile(t *testing.T) {
	producerCompiled := compileStagedExceptionHandling(t, stagedTagProductProducerModule())
	defer producerCompiled.Close()
	producer, err := instantiateCore(producerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := producer.ExportedTag("primary")
	if err != nil {
		t.Fatal(err)
	}

	mismatch := compileStagedExceptionHandling(t, stagedTagProductConsumerModule(0, 1))
	defer mismatch.Close()
	if in, err := instantiateCore(mismatch, InstantiateOptions{Imports: Imports{"env.first": primary, "env.second": primary}}); err == nil {
		_ = in.Close()
		t.Fatal("mismatched tag import instantiated")
	} else if !strings.Contains(err.Error(), "tag type") {
		t.Fatalf("mismatched tag import error = %v", err)
	}
	producer.lifeMu.Lock()
	refs := producer.resourceRefs
	producer.lifeMu.Unlock()
	if refs != 0 {
		t.Fatalf("failed tag link retained %d producer roots", refs)
	}
	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}
	producer.lifeMu.Lock()
	closed := producer.resourcesClosed
	producer.lifeMu.Unlock()
	if !closed {
		t.Fatal("failed tag link prevented producer resource release")
	}

	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.ExceptionHandling = true
	importedThrow, err := compileWithFrontendFeatures(cfg, stagedImportedTagThrowModule(), features)
	if err != nil {
		t.Fatalf("imported tag throw compile: %v", err)
	}
	_ = importedThrow.Close()
}

func TestStagedCrossInstanceExceptionHandlerTransfer(t *testing.T) {
	providerCompiled := compileStagedExceptionHandling(t, stagedCrossInstanceEHProviderModule())
	defer providerCompiled.Close()
	provider, err := instantiateCore(providerCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate EH provider: %v", err)
	}
	tag, err := provider.ExportedTag("tag")
	if err != nil {
		t.Fatal(err)
	}
	thrower, err := provider.ExportedFunc("throw")
	if err != nil {
		t.Fatal(err)
	}

	consumerCompiled := compileStagedExceptionHandling(t, stagedCrossInstanceEHConsumerModule())
	defer consumerCompiled.Close()
	newConsumer := func() *Instance {
		in, err := instantiateCore(consumerCompiled, InstantiateOptions{Imports: Imports{
			"env.tag":       tag,
			"env.tag-alias": tag,
			"env.throw":     thrower,
		}})
		if err != nil {
			t.Fatalf("instantiate EH consumer: %v", err)
		}
		return in
	}
	consumer := newConsumer()
	for _, name := range []string{"catch-imported", "catch-alias"} {
		got, err := consumer.Invoke(name)
		if err != nil || len(got) != 1 || uint32(got[0]) != 2 {
			t.Fatalf("%s result=%v err=%v, want 2", name, got, err)
		}
	}
	if got, err := consumer.Invoke("mismatch"); err != nil || len(got) != 1 || uint32(got[0]) != 3 {
		t.Fatalf("mismatch result=%v err=%v, want 3", got, err)
	}
	if got, err := thrower.inst.Invoke("throw"); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") || got != nil {
		t.Fatalf("uncaught provider throw result=%v err=%v", got, err)
	}
	if got, err := consumer.Invoke("catch-imported"); err != nil || uint32(got[0]) != 2 {
		t.Fatalf("cold recovery result=%v err=%v", got, err)
	}

	const workers = 8
	consumers := make([]*Instance, workers)
	for i := range consumers {
		consumers[i] = newConsumer()
	}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for _, in := range consumers {
		wg.Add(1)
		go func(in *Instance) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				got, err := in.Invoke("catch-imported")
				if err != nil || len(got) != 1 || uint32(got[0]) != 2 {
					errs <- errors.New("concurrent cross-instance catch failed")
					return
				}
			}
		}(in)
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}

	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	provider.lifeMu.Lock()
	closed := provider.resourcesClosed
	provider.lifeMu.Unlock()
	if closed {
		t.Fatal("EH provider resources closed while consumers retained function/tag roots")
	}
	for _, in := range consumers {
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := consumer.Close(); err != nil {
		t.Fatal(err)
	}
	provider.lifeMu.Lock()
	closed = provider.resourcesClosed
	provider.lifeMu.Unlock()
	if !closed {
		t.Fatal("EH provider resources remained live after final function/tag consumer close")
	}
}
