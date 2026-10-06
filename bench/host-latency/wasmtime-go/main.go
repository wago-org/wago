package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	wasmtime "github.com/bytecodealliance/wasmtime-go/v46"
)

func main() {
	args := os.Args[1:]
	work, batchMin := int32(4194304), int32(64)
	for len(args) > 0 && (strings.HasPrefix(args[0], "--work=") || strings.HasPrefix(args[0], "--batch-min=")) {
		key, value, _ := strings.Cut(args[0], "=")
		n, err := strconv.ParseInt(value, 10, 32)
		check(err)
		if n <= 0 {
			panic("work and batch minimum must be positive")
		}
		if key == "--work" {
			work = int32(n)
		} else {
			batchMin = int32(n)
		}
		args = args[1:]
	}
	if len(args) == 0 || args[0] != "--yield-atomic" {
		panic("expected --yield-atomic")
	}
	args = args[1:]
	counts := []int32{1, 65536}
	if len(args) != 0 {
		counts = nil
		for _, value := range args {
			n, err := strconv.ParseInt(value, 10, 32)
			check(err)
			if n <= 0 {
				panic("loop count must be positive")
			}
			counts = append(counts, int32(n))
		}
	}
	bytes, err := os.ReadFile("yield.wasm")
	check(err)
	engine := wasmtime.NewEngine()
	defer engine.Close()
	module, err := wasmtime.NewModule(engine, bytes)
	check(err)
	defer module.Close()
	for _, kind := range []string{"typed", "call", "caller"} {
		capture(engine, module, kind, counts, work, batchMin)
	}
}

func capture(engine *wasmtime.Engine, module *wasmtime.Module, kind string, counts []int32, work, batchMin int32) {
	store := wasmtime.NewStore(engine)
	defer store.Close()
	var counter atomic.Uint64
	var callback *wasmtime.Func
	switch kind {
	case "typed":
		callback = wasmtime.WrapFunc(store, func(v int32) int32 { counter.Add(1); return v + 1 })
	case "caller":
		callback = wasmtime.WrapFunc(store, func(_ *wasmtime.Caller, v int32) int32 { counter.Add(1); return v + 1 })
	case "call":
		typeI32 := wasmtime.NewValType(wasmtime.KindI32)
		typeFn := wasmtime.NewFuncType([]*wasmtime.ValType{typeI32}, []*wasmtime.ValType{typeI32})
		// Results are consumed synchronously. Reuse this numeric slot to avoid
		// charging an avoidable allocation to the explicit callback API.
		var result [1]wasmtime.Val
		callback = wasmtime.NewFunc(store, typeFn, func(_ *wasmtime.Caller, args []wasmtime.Val) ([]wasmtime.Val, *wasmtime.Trap) {
			counter.Add(1)
			result[0] = wasmtime.ValI32(args[0].I32() + 1)
			return result[:], nil
		})
	}
	instance, err := wasmtime.NewInstance(store, module, []wasmtime.AsExtern{callback})
	check(err)
	run := instance.GetFunc(store, "run")
	if run == nil {
		panic("missing run export")
	}
	for _, n := range counts {
		repeats := max(work/n, batchMin)
		invoke := func() {
			got, err := run.Call(store, n, int32(1))
			check(err)
			value, ok := got.(int32)
			if !ok || value != n {
				panic(fmt.Sprintf("result %v; want %d", got, n))
			}
		}
		for i := 0; i < 16; i++ {
			invoke()
		}
		for sample := 0; sample < 5; sample++ {
			counter.Store(0)
			start := time.Now()
			for i := int32(0); i < repeats; i++ {
				invoke()
			}
			ns := float64(time.Since(start).Nanoseconds()) / (float64(repeats) * float64(n))
			if counter.Load() != uint64(repeats)*uint64(n) {
				panic("wrong callback count")
			}
			fmt.Printf("wasmtime-go,%s-yield-atomic,%d,1,%d,%.4f\n", kind, n, sample, ns)
		}
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
