#!/usr/bin/env bash
set -euo pipefail
repo=$(realpath "${1:-$PWD}")
base=${2:-9fe81ea747a852cb165d16685181d148249011f2}
head=${3:-9fdedcb36288fcab5edaff8956b5363366f9971b}
out=$(realpath -m "${4:-${RUNNER_TEMP:-/tmp}/specialization-evidence}")
campaign=$(realpath "${5:-$repo/.campaign}")
mkdir -p "$out" "$campaign/locks"
case $(uname -m) in x86_64) ;; *) echo 'Native AMD64 required' >&2; exit 2;; esac
export GOMAXPROCS=4
{ date -u; uname -a; go version; lscpu; printf 'baseline=%s\ncandidate=%s\n' "$base" "$head"; } > "$out/environment.txt"
base_added=0
head_added=0
cleanup() {
 if (( base_added )); then git -C "$repo" worktree remove --force "$out/base-tree"; fi
 if (( head_added )); then git -C "$repo" worktree remove --force "$out/head-tree"; fi
}
trap cleanup EXIT
git -C "$repo" worktree add --detach "$out/base-tree" "$base"
base_added=1
git -C "$repo" worktree add --detach "$out/head-tree" "$head"
head_added=1
# Focused reference oracle only; no full corpus suite is claimed by this driver.
mkdir -p "$out/baseline-harness"
for file in src/wago/gc_native_i31_none_test.go src/wago/gc_native_i31_none_stats_amd64_test.go src/wago/gc_native_i31_none_stats_arm64_test.go; do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
 cp "$out/base-tree/$file" "$out/baseline-harness/$(basename "$file")"
done
cat > "$out/oracle-source_test.go" <<'SHARED_ORACLE_GO'
//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNativeI31NoneModule() []byte {
	types := [][]byte{{0x5f, 0}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.I32})}
	funcs := [][]byte{{1}}
	exports := [][]byte{wasmtest.ExportEntry("init", 0, 0)}
	bodies := [][]byte{wasmtest.Code([]byte{
		0x41, 1, 0x20, 0, 0xfb, 28, 0x26, 0, // table[1] = i31(parameter)
		0x41, 2, 0xfb, 1, 0, 0x26, 0, // table[2] = struct.new_default
		0x0b,
	})}
	for _, heap := range []byte{0x6c, 0x71, 0x6b} { // i31, none, struct (helper control)
		for _, op := range []byte{20, 21, 22, 23} {
			body := []byte{0x20, 0, 0x25, 0, 0xfb, op, heap}
			if op >= 22 {
				body = append(body, 0xd1) // ref.is_null also checks the cast result
			}
			body = append(body, 0x0b)
			funcs = append(funcs, []byte{2})
			exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("op%d_%x", op, heap), 0, uint32(len(funcs)-1)))
			bodies = append(bodies, wasmtest.Code(body))
			foreignBody := []byte{0x20, 0, 0xfb, 26, 0xfb, op, heap}
			if op >= 22 {
				foreignBody = append(foreignBody, 0xd1)
			}
			foreignBody = append(foreignBody, 0x0b)
			funcs = append(funcs, []byte{3})
			exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("foreign%d_%x", op, heap), 0, uint32(len(funcs)-1)))
			bodies = append(bodies, wasmtest.Code(foreignBody))
		}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6e, 0, 4})),
		wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCNativeI31None(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeI31NoneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range []uint64{0, 1, 0x3fffffff, 0x40000000, 0x7fffffff, 0xffffffff} {
			foreign, err := in.NewExternRef(fmt.Sprintf("foreign-%x", x))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := in.Invoke("init", x); err != nil {
				t.Fatal(err)
			}
			for round := 0; round < 2; round++ {
				for _, heap := range []byte{0x6c, 0x71, 0x6b} {
					for _, op := range []byte{20, 21, 22, 23} {
						foreignName := fmt.Sprintf("foreign%d_%x", op, heap)
						foreignGot, foreignErr := in.Invoke(foreignName, ValueExternRef(foreign).Bits())
						if op < 22 {
							if foreignErr != nil || len(foreignGot) != 1 || foreignGot[0] != 0 {
								t.Fatalf("%s: %v, %v", foreignName, foreignGot, foreignErr)
							}
						} else {
							var trap *TrapError
							if !errors.As(foreignErr, &trap) || trap.Code != TrapCastFailure {
								t.Fatalf("%s: %v, %v; want cast failure", foreignName, foreignGot, foreignErr)
							}
						}
						for index := uint64(0); index < 3; index++ {
							match := index == 1 && heap == 0x6c || index == 2 && heap == 0x6b || index == 0 && op&1 != 0
							name := fmt.Sprintf("op%d_%x", op, heap)
							got, err := in.Invoke(name, index)
							if op >= 22 && !match {
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
									t.Fatalf("%s(%d): %v, %v; want cast failure", name, index, got, err)
								}
								continue
							}
							want := uint64(0)
							if op < 22 && match || op >= 22 && index == 0 {
								want = 1
							}
							if err != nil || len(got) != 1 || got[0] != want {
								t.Fatalf("%s(%d): %v, %v; want %d", name, index, got, err, want)
							}
						}
					}
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
		}
		in.Close()
	}
}

func TestGCNativeI31NoneHighBitsAndHandles(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeI31NoneModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	// Inject words after host admission to check the native boundary directly.
	// No allocation or collection occurs while the table contains a forged word.
	table := unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)
	for _, word := range []uint64{1, 0xffffffff, 0x100000000, 0x100000001, 0xffffffff00000000, 0xffffffffffffffff, 0xfffffffe} {
		binary.LittleEndian.PutUint64(table[8:], word)
		for _, heap := range []byte{0x6c, 0x71} {
			kind := gc.RefTestI31
			if heap == 0x71 {
				kind = gc.RefTestNone
			}
			for _, op := range []byte{20, 21, 22, 23} {
				target := gc.RefTestTarget{Kind: kind, Nullable: op&1 != 0}
				match, helperErr := in.gc.RefTest(gc.Ref(uint32(word)), target)
				if state := in.existingGCRefTestTableState(); state != nil {
					match, helperErr = state.refTest(in.gc, word, target)
				}
				got, err := in.Invoke(fmt.Sprintf("op%d_%x", op, heap), 0)
				if helperErr != nil {
					if err == nil {
						t.Fatalf("word%x op%d heap%x: helper error was hidden: %v", word, op, heap, got)
					}
				} else if op >= 22 && !match {
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
						t.Fatalf("word%x op%d heap%x: %v; want cast failure", word, op, heap, err)
					}
				} else {
					want := uint64(0)
					if op < 22 && match || op >= 22 && uint32(word) == 0 {
						want = 1
					}
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("word%x op%d heap%x: %v,%v; want %d", word, op, heap, got, err, want)
					}
				}
			}
		}
		binary.LittleEndian.PutUint64(table[8:], 0)
	}
}

func TestGCNativeI31NoneTrapOrderAndLivePrefix(t *testing.T) {
	for _, op := range []byte{20, 21, 22, 23} {
		params := make([]wasm.ValType, 17)
		results := make([]wasm.ValType, 17)
		body := []byte{0x41, 1, 0x20, 0, 0x6e} // division remains the first trap
		for i := range params {
			params[i], results[i] = wasm.I32, wasm.I32
		}
		for i := 1; i < len(params); i++ {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0x23, 0, 0xfb, op, 0x6c, 0x1a, 0x0b)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0}, wasmtest.FuncType(params, results))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		args := make([]uint64, len(params))
		for i := 1; i < len(args); i++ {
			args[i] = uint64(100 + i)
		}
		_, err = in.Invoke("run", args...)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapDivZero {
			t.Fatalf("op%d: %v; want division trap", op, err)
		}
		args[0] = 1
		got, err := in.Invoke("run", args...)
		if op == 22 {
			if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
				t.Fatalf("op%d: %v; want cast failure", op, err)
			}
		} else {
			if err != nil || len(got) != len(args) {
				t.Fatalf("op%d prefix: %v,%v", op, got, err)
			}
			for i := range args {
				if got[i] != args[i] {
					t.Fatalf("op%d prefix%d: %d; want %d", op, i, got[i], args[i])
				}
			}
		}
		in.Close()
		c.Close()
	}
}

func gcNativeI31NoneLoop(op, heap byte, mixed bool) []byte {
	return gcNativeI31NoneLoopValue(op, heap, mixed, false)
}

func gcNativeI31NoneLoopValue(op, heap byte, mixed, object bool) []byte {
	global := []byte{0x6e, 1, 0x41, 7, 0xfb, 28, 0x0b}
	if object {
		global = []byte{0x6e, 1, 0xfb, 1, 0, 0x0b}
	}
	if heap == 0x71 {
		global = []byte{0x6e, 1, 0xd0, 0x6e, 0x0b}
	}
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x4f, 0x0d, 1}
	if mixed {
		body = append(body, 0x20, 1, 0x41, 1, 0x71, 0x04, 0x6e, 0x23, 0, 0x05, 0xd0, 0x6e, 0x0b)
	} else {
		body = append(body, 0x23, 0)
	}
	body = append(body, 0xfb, op, heap, 0x1a, 0x20, 1, 0x41, 1, 0x6a, 0x21, 1, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	return gcInstructionBenchmarkModule(body, global)
}

func BenchmarkGCNativeI31None(b *testing.B) {
	for _, tc := range []struct {
		name     string
		op, heap byte
		mixed    bool
	}{
		{"test-i31", 20, 0x6c, false}, {"test-none", 21, 0x71, false},
		{"cast-i31", 22, 0x6c, false}, {"cast-none", 23, 0x71, false},
		{"mixed-test", 21, 0x6c, true}, {"mixed-cast", 23, 0x6c, true},
		{"helper-struct", 20, 0x6b, false},
	} {
		b.Run(tc.name, func(b *testing.B) { benchmarkGCInstructionLoop(b, gcNativeI31NoneLoop(tc.op, tc.heap, tc.mixed), 1) })
	}
}

func BenchmarkGCNativeI31NoneCompile(b *testing.B) {
	data := gcNativeI31NoneModule()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(c.CodeSize()), "native-B")
		c.Close()
	}
}

func BenchmarkGCNativeI31NoneObject(b *testing.B) {
	for _, mixed := range []bool{false, true} {
		b.Run(fmt.Sprintf("mixed-%v", mixed), func(b *testing.B) { benchmarkGCInstructionLoop(b, gcNativeI31NoneLoopValue(21, 0x6c, mixed, true), 1) })
	}
}

func BenchmarkGCSharedClassifierCompileWorkers(b *testing.B) {
 for _,workers:=range []int{1,4} { b.Run(fmt.Sprint(workers),func(b *testing.B){
  cfg:=NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithFunctionWorkers(workers)
  data:=gcNativeI31NoneModule()
  b.ReportAllocs(); b.ResetTimer()
  for range b.N { c,err:=Compile(cfg,data); if err!=nil {b.Fatal(err)}; b.ReportMetric(float64(c.CodeSize()),"native-B"); c.Close() }
 }) }
}

SHARED_ORACLE_GO
for stage in base head; do cp "$out/oracle-source_test.go" "$out/$stage-tree/src/wago/gc_native_i31_none_test.go"; done
cp "$out/oracle-source_test.go" "$out/baseline-harness/gc_native_i31_none_test.go"
for stage in base head; do git -C "$out/$stage-tree" diff --binary > "$out/$stage-test-overlay.diff"; done
sha256sum "$out"/baseline-harness/* > "$out/baseline-harness-sha256.txt"
for file in "$out"/baseline-harness/*; do git diff --no-index -- /dev/null "$file" >> "$out/baseline-test-overlay.diff" || test "$?" -eq 1; done
for stage in base head; do
 ( cd "$out/$stage-tree"; python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago )
done
( cd "$out/head-tree"; python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck,wago_codegenstats -o "$out/head-debug.test" ./src/wago )
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" > "$out/binary-sha256.txt"
( cd "$out/base-tree/src/wago"; python3 "$campaign/run.py" build "$out/base.test" -test.run '^TestGCNativeI31None' -test.v > "$out/base-oracle.txt" 2>&1 )
for stage in head head-debug; do
 ( cd "$out/head-tree/src/wago"; python3 "$campaign/run.py" build "$out/$stage.test" -test.run '^TestGCNativeI31None' -test.v > "$out/$stage-correctness.txt" 2>&1 )
done
cat > "$out/probe-counter.go" <<'PROBE_SOURCE'
package wago
var campaignRefEntries uint64

PROBE_SOURCE
cat > "$out/probe_test.go" <<'PROBE_SOURCE'
package wago
import ("testing"; "os"; "path/filepath"; "crypto/sha256"; "fmt")
func TestCampaignRefProbe(t *testing.T) {
 root := os.Getenv("CAMPAIGN_REF_PROBE_DIR")
 if err:=os.MkdirAll(root,0755); err!=nil {t.Fatal(err)}
 cases := []struct{name string;op,heap byte;mixed,object bool}{
 {"test-i31",20,0x6c,false,false},{"test-none",21,0x71,false,false},
 {"cast-i31",22,0x6c,false,false},{"cast-none",23,0x71,false,false},
 {"fallback-object",21,0x6c,false,true},{"mixed-object",21,0x6c,true,true},
 {"control-struct",20,0x6b,false,false},
 }
 for _,tc := range cases {
  c,err:=Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3),gcNativeI31NoneLoopValue(tc.op,tc.heap,tc.mixed,tc.object)); if err!=nil {t.Fatal(err)}
  in,err:=Instantiate(c,InstantiateOptions{}); if err!=nil {t.Fatal(err)}
  campaignRefEntries=0
  if got,err:=in.Invoke("run",1000);err!=nil || len(got)!=1 ||got[0]!=1000 {t.Fatalf("%s: %v,%v",tc.name,got,err)}
  t.Logf("%s helpers=%d operations=1000 native_bytes=%d code_sha256=%x",tc.name,campaignRefEntries,c.CodeSize(),sha256.Sum256(c.code))
  if err:=os.WriteFile(filepath.Join(root,tc.name+".bin"),c.code,0644);err!=nil {t.Fatal(err)}
  in.Close();c.Close()
 }
 c,err:=Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3),gcNativeI31NoneModule());if err!=nil {t.Fatal(err)};defer c.Close()
 in,err:=Instantiate(c,InstantiateOptions{});if err!=nil {t.Fatal(err)};defer in.Close()
 foreign,err:=in.NewExternRef("foreign-probe");if err!=nil {t.Fatal(err)}
 for _,op:=range []byte{20,21,22,23}{
 campaignRefEntries=0
 for i:=0;i<100;i++ {_,err=in.Invoke(fmt.Sprintf("foreign%d_6c",op),ValueExternRef(foreign).Bits());if op<22 &&err!=nil {t.Fatal(err)};if op>=22 &&err==nil {t.Fatal("missing trap")}}
 t.Logf("foreign-op%d helpers=%d operations=100",op,campaignRefEntries)
 }
}

PROBE_SOURCE
for stage in base head; do
 python3 - "$out/$stage-tree" "$out" <<'PY_OVERLAY'
import pathlib,sys,json
src=pathlib.Path(sys.argv[1]); out=pathlib.Path(sys.argv[2]); p=src/'src/wago/gc_struct_helper.go'
s=p.read_text();needle='func (in *Instance) dispatchGCStructHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) {'
assert needle in s;s=s.replace(needle,needle+'\n\tif helper == gcStructRefTest || helper == gcStructRefCast { campaignRefEntries++ }',1)
saved=out/(src.name+'-instrumented-helper.go');saved.write_text(s)
mapping={str(p):str(saved),str(src/'src/wago/campaign_ref_counter.go'):str(out/'probe-counter.go'),str(src/'src/wago/campaign_ref_probe_test.go'):str(out/'probe_test.go')}
(out/(src.name+'-probe-overlay.json')).write_text(json.dumps({'Replace':mapping}))
PY_OVERLAY
 ( cd "$out/$stage-tree"; python3 "$campaign/run.py" build go test -p 4 -overlay="$out/$stage-tree-probe-overlay.json" -c -o "$out/$stage-probe.test" ./src/wago )
 ( cd "$out/$stage-tree/src/wago"; python3 "$campaign/run.py" build env CAMPAIGN_REF_PROBE_DIR="$out/$stage-code" "$out/$stage-probe.test" -test.run '^TestCampaignRefProbe$' -test.v > "$out/$stage-helper-code-probe.txt" 2>&1 )
done
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" "$out/base-probe.test" "$out/head-probe.test" > "$out/binary-sha256.txt"
sha256sum "$out"/*instrumented-helper.go "$out"/*probe-overlay.json "$out/probe-counter.go" "$out/probe_test.go" "$out/oracle-source_test.go" > "$out/overlay-sha256.txt"
for binary in base head head-debug base-probe head-probe; do go version -m "$out/$binary.test" > "$out/$binary-buildinfo.txt"; done
python3 - "$out/head-debug-buildinfo.txt" <<'CHECK_TAG'
import pathlib,sys
assert '-tags=wago_regalloccheck,wago_codegenstats' in pathlib.Path(sys.argv[1]).read_text()
CHECK_TAG
if [[ "${SHARED_PREFLIGHT_ONLY:-0}" == 1 ]]; then exit 0; fi
python3 "$campaign/run.py" bench python3 - "$out" <<'PY_BENCH'
import os,subprocess,pathlib,sys,time,json
r=pathlib.Path(sys.argv[1]); allowed=sorted(os.sched_getaffinity(0)); os.sched_setaffinity(0,{allowed[0]}); (r/'cpu-affinity.txt').write_text(str(allowed[0]))
patterns=['^(BenchmarkGCNativeI31None(|Compile|Object)|BenchmarkGCSharedClassifierCompileWorkers|BenchmarkGCInstructionLoopControl|BenchmarkInvokeHostFuncDirect)$','^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host[01]$/^n(0|64|1024)$']
def external_go():
 return [line for line in subprocess.check_output(['ps','-eo','comm,args'],text=True).splitlines() if line.split(None,1)[0] in ('go','compile','link')]
for pair in range(7):
 attempt=0
 while True:
  while external_go(): time.sleep(1)
  before=external_go(); attempt+=1
  for stage in (['base','head'] if pair%2==0 else ['head','base']):
   parts=[]
   for pattern in patterns:
    p=subprocess.run([str(r/(stage+'.test')),'-test.run=^$','-test.bench='+pattern,'-test.benchmem','-test.benchtime=200ms','-test.count=1'],cwd=str(r/(stage+'-tree')/'src/wago'),text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    parts.append(p.stdout)
    if p.returncode: raise SystemExit(p.returncode)
   (r/(stage+'-sample'+str(pair)+'.txt')).write_text(''.join(parts))
  after=external_go()
  (r/('process-pair'+str(pair)+'-attempt'+str(attempt)+'.json')).write_text(json.dumps({'before':before,'after':after}))
  if not before and not after: break
  for stage in ['base','head']:
   (r/(stage+'-sample'+str(pair)+'.txt')).rename(r/(stage+'-sample'+str(pair)+'-contaminated'+str(attempt)+'.txt'))
  if attempt>=10: raise SystemExit('repeated external build contamination')

PY_BENCH
