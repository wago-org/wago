package main

import (
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wago-org/wago/src/wago"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--entry" || os.Args[1] == "--entry-host") {
		entry(os.Args[1] == "--entry-host")
		return
	}
	yielding, unbounded, withAtomic, noLeaf, multi := false, false, false, false, false
	args := os.Args[1:]
	api := "instance"
	if len(args) > 0 && strings.HasPrefix(args[0], "--api=") {
		api = strings.TrimPrefix(args[0], "--api=")
		if api != "instance" && api != "prepared" && api != "session" {
			panic("API must be instance, prepared or session")
		}
		args = args[1:]
	}
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
	if len(args) > 0 && (args[0] == "--yield" || args[0] == "--yield-unbounded" || args[0] == "--yield-atomic" || args[0] == "--yield-no-leaf" || args[0] == "--multi") {
		yielding, unbounded, withAtomic, multi = true, args[0] == "--yield-unbounded", args[0] == "--yield-atomic" || args[0] == "--multi", args[0] == "--multi"
		noLeaf = args[0] == "--yield-no-leaf"
		args = args[1:]
	}
	counts := []int32{1, 1024, 65536}
	if len(args) > 0 {
		counts = nil
		for _, arg := range args {
			n, err := strconv.ParseInt(arg, 10, 32)
			check(err)
			if n <= 0 {
				panic("loop count must be positive")
			}
			counts = append(counts, int32(n))
		}
	}
	filename := "loop.wasm"
	if yielding {
		filename = "yield.wasm"
	}
	if multi {
		filename = "multi.wasm"
	}
	bytes, err := os.ReadFile(filename)
	check(err)
	cfg := wago.NewRuntimeConfig()
	if unbounded {
		cfg = cfg.WithOptimization("prepared-bounded-entry", false)
	}
	if (noLeaf || unbounded) && goruntime.GOARCH == "arm64" {
		cfg = cfg.WithOptimization("native-leaf-host", false)
	}
	c, err := wago.Compile(cfg, bytes)
	check(err)
	defer c.Close()
	for _, kind := range []string{"typed", "call", "caller"} {
		var counter, counter2 atomic.Uint64
		var fn any = func(v int32) int32 { return v + 1 }
		if withAtomic {
			fn = func(v int32) int32 { counter.Add(1); return v + 1 }
		}
		if kind == "caller" {
			fn = func(_ wago.Caller, call wago.HostCall) { call.SetI32(0, call.I32(0)+1) }
			if withAtomic {
				fn = func(_ wago.Caller, call wago.HostCall) { counter.Add(1); call.SetI32(0, call.I32(0)+1) }
			}
		} else if kind == "call" {
			fn = func(call wago.HostCall) { call.SetI32(0, call.I32(0)+1) }
			if withAtomic {
				fn = func(call wago.HostCall) { counter.Add(1); call.SetI32(0, call.I32(0)+1) }
			}
		}
		imports := wago.NewImports()
		imports.HostFunc("env", "step", fn).Params(wago.ValI32).Results(wago.ValI32)
		if multi {
			var fn2 any = func(v int32) int32 { counter2.Add(1); return v + 1 }
			if kind == "call" {
				fn2 = func(call wago.HostCall) { counter2.Add(1); call.SetI32(0, call.I32(0)+1) }
			}
			if kind == "caller" {
				fn2 = func(_ wago.Caller, call wago.HostCall) { counter2.Add(1); call.SetI32(0, call.I32(0)+1) }
			}
			imports.HostFunc("env", "step2", fn2).Params(wago.ValI32).Results(wago.ValI32)
		}
		in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
		check(err)
		var prepared *wago.WasmFunc
		var session *wago.PreparedSession
		if api != "instance" {
			prepared, err = in.WasmFunc("run")
			check(err)
			if api == "session" {
				session, err = prepared.OpenSession()
				check(err)
			}
		}
		hosts := []int32{0, 1}
		if yielding {
			hosts = []int32{1}
		}
		for _, n := range counts {
			repeats := max(work/n, batchMin)
			for _, host := range hosts {
				invoke := func() {
					var got []uint64
					var err error
					switch api {
					case "prepared":
						got, err = prepared.Invoke(wago.I32(n), wago.I32(host))
					case "session":
						got, err = session.Invoke2(wago.I32(n), wago.I32(host))
					default:
						got, err = in.Invoke("run", wago.I32(n), wago.I32(host))
					}
					check(err)
					if len(got) != 1 || got[0] != uint64(n) {
						panic(fmt.Sprintf("run: %v; want %d", got, n))
					}
				}
				for i := 0; i < 16; i++ {
					invoke()
				}
				for sample := 0; sample < 5; sample++ {
					counter.Store(0)
					counter2.Store(0)
					start := time.Now()
					for i := int32(0); i < repeats; i++ {
						invoke()
					}
					ns := float64(time.Since(start).Nanoseconds()) / (float64(repeats) * float64(n))
					if withAtomic && !multi && counter.Load() != uint64(repeats)*uint64(n) {
						panic("wrong atomic callback count")
					}
					if multi && (counter.Load() != uint64(repeats)*uint64(n/2+n%2) || counter2.Load() != uint64(repeats)*uint64(n/2)) {
						panic("wrong per-import callback counts")
					}
					label := kind
					if api != "instance" {
						label += "-" + api
					}
					if multi {
						label += "-multi"
					}
					if yielding {
						label += "-yield"
					}
					if unbounded {
						label += "-unbounded"
					}
					if withAtomic {
						label += "-atomic"
					}
					if noLeaf {
						label += "-no-leaf"
					}
					fmt.Printf("wago,%s,%d,%d,%d,%.4f\n", label, n, host, sample, ns)
				}
			}
		}
		if session != nil {
			session.Close()
		}
		check(in.Close())
	}
}

func entry(withHost bool) {
	filename := "entry.wasm"
	label := "entry"
	if withHost {
		filename = "entry-host.wasm"
		label = "entry-host"
	}
	bytes, err := os.ReadFile(filename)
	check(err)
	c, err := wago.Compile(wago.NewRuntimeConfig(), bytes)
	check(err)
	defer c.Close()
	opts := wago.InstantiateOptions{}
	if withHost {
		imports := wago.NewImports()
		imports.HostFunc("env", "step", func(v int32) int32 { return v + 1 }).Params(wago.ValI32).Results(wago.ValI32)
		opts.Imports = imports
	}
	in, err := wago.Instantiate(c, opts)
	check(err)
	defer in.Close()
	fn, err := in.WasmFunc("add")
	check(err)
	for _, mode := range []string{"instance", "prepared", "session"} {
		var session *wago.PreparedSession
		if mode == "session" {
			session, err = fn.OpenSession()
			check(err)
		}
		invoke := func(input uint64) {
			var got []uint64
			var err error
			switch mode {
			case "instance":
				got, err = in.Invoke("add", input)
			case "prepared":
				got, err = fn.Invoke(input)
			case "session":
				got, err = session.Invoke1(input)
			}
			check(err)
			if len(got) != 1 || got[0] != input+1 {
				panic("bad entry result")
			}
		}
		for i := 0; i < 1024; i++ {
			invoke(41)
		}
		for sample := 0; sample < 10; sample++ {
			start := time.Now()
			for i := 0; i < 4194304; i++ {
				invoke(uint64(i & 1023))
			}
			ns := float64(time.Since(start).Nanoseconds()) / 4194304
			fmt.Printf("wago,%s-%s,1,1,%d,%.4f\n", label, mode, sample, ns)
		}
		if session != nil {
			session.Close()
		}
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
