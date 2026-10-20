#!/usr/bin/env bash
set -euo pipefail
case $(uname -m) in aarch64|arm64|x86_64) ;; *) echo 'Native AMD64 or ARM64 required' >&2; exit 2;; esac
test "$(go env GOARCH)" = "$(go env GOHOSTARCH)"
repo=$(git rev-parse --show-toplevel)
base=7997c86c36e05cfba4c4f39cabb026a318f55fc0
head=597d8e6e7b837989e929ced973bf4ed4e9e87303
out="${RUNNER_TEMP:?}/specialization-evidence"
mkdir -p "$out/campaign/locks" "$out/campaign/evidence" "$out/base-code" "$out/head-code"
cp "$repo/.campaign/run.py" "$out/campaign/run.py"
export GOMAXPROCS=4
{
 date -u
 uname -a
 go version
 go env GOOS GOARCH
 lscpu
 printf 'base=%s\nhead=%s\n' "$base" "$head"
} > "$out/environment.txt"
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
for file in src/wago/shared_grow_dynamic_failure_test.go src/wago/shared_grow_dynamic_failure_profile_test.go; do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
done
git -C "$out/base-tree" add -N src/wago/shared_grow_dynamic_failure_test.go src/wago/shared_grow_dynamic_failure_profile_test.go
# The baseline intentionally expects every grow to enter Go.
python3 - "$out/base-tree/src/wago/shared_grow_dynamic_failure_profile_test.go" <<'PYBASE'
import sys,pathlib
p=pathlib.Path(sys.argv[1]);s=p.read_text().replace('{"proven-failure", 3, 0, 0}', '{"proven-failure", 3, 0, iterations}').replace('{"mixed-failures", 1, 2, iterations / 2}', '{"mixed-failures", 1, 2, iterations}').replace('{{(1 << 40) + 1, 0}', '{{(1 << 40) + 1, 1}')
p.write_text(s)
PYBASE
cat > "$out/mem02b_evidence_test.go" <<'DUMPHARNESS'
//go:build (linux || darwin) && (amd64 || arm64) && !tinygo
package wago
import("testing"; "os"; "fmt"; "path/filepath"; "context"; "errors"; "runtime"; "sync/atomic"; "time"; "unsafe"; "github.com/wago-org/wago/src/core/compiler/wasm"; "github.com/wago-org/wago/tests/support/wasmtest")
func TestMem02BDump(t *testing.T) {
 dir:=os.Getenv("MEM02B_DUMP_DIR"); if dir=="" {t.Skip("evidence only")}
 for _, wide:=range []bool{false,true} {for _, index:=range []byte{0,1} {
 c,err:=sharedGrowDynamicFailureConfig().Compile(sharedGrowDynamicFailureModule(wide,index,false));if err!=nil{t.Fatal(err)}
 path:=filepath.Join(dir,fmt.Sprintf("i64-%t-index-%d.bin",wide,index));if err:=os.WriteFile(path,c.code,0600);err!=nil{t.Fatal(err)}
 t.Logf("%s %d native bytes",path,len(c.code)); c.Close()
 }}
}
func mem02bPureLoopModule(wide bool, index byte, imported bool) []byte {
	return mem02bPureLoopModuleWithMaximum(wide, index, imported, 3)
}
func mem02bPureLoopModuleWithMaximum(wide bool, index byte, imported bool, maximum uint64) []byte {
	typ, limits, wire, constant := wasm.I32, byte(3), byte(0x7f), byte(0x41)
	if wide {
		typ, limits, wire, constant = wasm.I64, 7, 0x7e, 0x42
	}
	selected := []byte{limits, 1}
	for remaining := maximum; ; {
		b := byte(remaining & 0x7f)
		remaining >>= 7
		if remaining != 0 {
			b |= 0x80
		}
		selected = append(selected, b)
		if remaining == 0 {
			break
		}
	}
	memories := [][]byte{selected, {limits ^ 6, 0, 1}}
	if index != 0 {
		memories[0], memories[1] = memories[1], memories[0]
	}
	body := func(raw []byte) []byte { return append(wasmtest.ULEB(uint32(len(raw))), raw...) }
	run := []byte{1, 1, 0x7e, 0x20, 1, 0x42, 1, 0x7c, 0x21, 2, 0x20, 1, 0x42, 2, 0x7c, 0x20, 0, 0x40, index, 0x20, 2, 0x0b}
	loop := []byte{2, 1, 0x7f, 1, wire, 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 0, 0x4f, 0x0d, 1, 0x20, 4, 0x20, 3}
	and, or, add := byte(0x71), byte(0x72), byte(0x6a)
	if wide {
		loop = append(loop, 0xad)
		and, or, add = 0x83, 0x84, 0x7c
	}
	loop = append(loop, 0x20, 2, and, 0x20, 1, or, 0x40, index, add, 0x21, 4, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 4, 0x0b)
	// The host can release this loop after an interruption failure. This keeps
	// failed cancellation tests from retaining a native execution indefinitely.
	spin := []byte{constant, 4, 0x41, 1, 0xfe, 0x17, 0x42, index, 0, 0x03, 0x40,
		constant, 4, 0xfe, 0x10, 0x42, index, 0, 0x41, 1, 0x46, 0x0d, 0, 0x0b, 0x0b}
	trap := []byte{1, 1, 0x7e, constant, 0x7f, 0x28, 0x42, index, 0, 0x1a}
	trap = append(trap, run[3:]...)
	vector := []byte{0xfd, 12, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 0x20, 0, 0x40, index, 0x0b}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{typ, wasm.I64}, []wasm.ValType{wasm.I64, typ, wasm.I64}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, typ}, []wasm.ValType{typ}),
		wasmtest.FuncType([]wasm.ValType{typ}, nil),
		wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.V128, typ})))}
	if imported {
		var entries [][]byte
		for n, m := range memories {
			entry := append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("memory%d", n))...)
			entries = append(entries, append(append(entry, 2), m...))
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entries...)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{2}, []byte{0}, []byte{3})))
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(memories...)))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(
		wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("loop", 0, 1), wasmtest.ExportEntry("spin", 0, 2), wasmtest.ExportEntry("trap-before", 0, 3), wasmtest.ExportEntry("vector", 0, 4),
		wasmtest.ExportEntry("memory", 2, uint32(index)), wasmtest.ExportEntry("memory0", 2, 0), wasmtest.ExportEntry("memory1", 2, 1))),
		wasmtest.Section(10, wasmtest.Vec(body(run), body(loop), wasmtest.Code(spin), body(trap), wasmtest.Code(vector))))
	return wasmtest.Module(sections...)
}


func TestMem02BPureLoopCancellation(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			t.Run(fmt.Sprintf("wide=%t/index=%d", wide, index), func(t *testing.T) {
				c, err := sharedGrowDynamicFailureConfig().Compile(mem02bPureLoopModule(wide, index, false))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				memory, err := in.ExportedMemory("memory")
				if err != nil {
					in.Close()
					t.Fatal(err)
				}
				flag := (*uint32)(unsafe.Pointer(&memory.UnsafeBytes()[4]))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				watchdog := time.AfterFunc(5*time.Second, cancel)
				defer watchdog.Stop()
				done := make(chan error, 1)
				go func() { _, err := in.InvokeContext(ctx, "spin", 3); done <- err }()
				for atomic.LoadUint32(flag) == 0 {
					select {
					case err := <-done:
						in.Close()
						t.Fatalf("spin returned before native entry:%v", err)
					case <-ctx.Done():
						t.Fatal("native entry timed out")
					default:
						runtime.Gosched()
					}
				}
				cancel()
				select {
				case err := <-done:
					in.Close()
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel:%v", err)
					}
				case <-time.After(5 * time.Second):
					atomic.StoreUint32(flag, 2)
					select {
					case <-done:
						in.Close()
					case <-time.After(5 * time.Second):
						t.Fatal("native loop ignored cancellation and the stop flag")
					}
					t.Fatal("native loop ignored cancellation")
				}
			})
		}
	}
}


DUMPHARNESS
for stage in base head; do
 cp "$out/mem02b_evidence_test.go" "$out/$stage-tree/src/wago/mem02b_evidence_test.go"
 (
  cd "$out/$stage-tree"
  git add -N src/wago/mem02b_evidence_test.go
  git diff --binary > "$out/$stage-test-overlay.diff"
  sha256sum src/wago/shared_grow_dynamic_failure_test.go src/wago/shared_grow_dynamic_failure_profile_test.go src/wago/mem02b_evidence_test.go > "$out/$stage-harness-sha256.txt"
  git rev-parse HEAD > "$out/$stage-commit.txt"
  python3 "$out/campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago
  python3 "$out/campaign/run.py" build go test -p 4 -c -tags=wago_profile,wago_regalloccheck -o "$out/$stage-profile.test" ./src/wago
  cd src/wago
  python3 "$out/campaign/run.py" build env MEM02B_DUMP_DIR="$out/$stage-code" "$out/$stage.test" -test.run '^(TestMem02BDump|TestMem02BPureLoopCancellation|TestSharedGrowDynamicFailure.*|TestThreadsSharedMemoryConcurrentGrowth|TestThreadsSharedMemoryGrowToMemory32Limit)$' -test.v -test.count=1 -test.timeout=180s > "$out/$stage-correctness.txt" 2>&1
  python3 "$out/campaign/run.py" build "$out/$stage-profile.test" -test.run '^TestSharedGrowDynamicFailure' -test.v -test.count=1 -test.timeout=180s > "$out/$stage-helpers.txt" 2>&1
 )
done
sha256sum "$out/"*.test > "$out/binary-sha256.txt"
sha256sum "$out/"*-test-overlay.diff "$repo/.campaign/native-bench.sh" > "$out/build-input-sha256.txt"
python3 "$out/campaign/run.py" bench python3 - "$out" <<'BENCH'
import sys,pathlib,os,subprocess,threading,time,json
out=pathlib.Path(sys.argv[1]);available=sorted(os.sched_getaffinity(0));os.sched_setaffinity(0,set(available[:min(4,len(available))]))
(out/'affinity.json').write_text(json.dumps({'available':available,'used':sorted(os.sched_getaffinity(0))}))
for pair in range(1,8):
 for stage in (('base','head') if pair%2 else ('head','base')):
  stop=threading.Event()
  def monitor():
   with (out/f'process-{pair}-{stage}.jsonl').open('w') as log:
    while not stop.is_set():
     rows=subprocess.check_output(['ps','-eo','pid,ppid,pcpu,comm'],text=True)
     log.write(json.dumps({'time':time.time(),'loadavg':os.getloadavg(),'processes':rows})+'\n');log.flush();stop.wait(1)
  thread=threading.Thread(target=monitor);thread.start()
  try:
   with (out/f'pair-{pair}-{stage}.txt').open('w') as log:
    commands=[['-test.bench=^BenchmarkSharedGrowDynamicFailure(Loop|Compile)$','-test.benchtime=200ms'],['-test.bench=^BenchmarkInvokeHostFuncDirect$','-test.benchtime=200ms'],['-test.bench=^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host(0|1)$/^n(0|64|1024)$','-test.benchtime=200ms']]
    for command in commands:
     subprocess.run([str(out/f'{stage}.test'),'-test.run=^$','-test.benchmem','-test.count=1',*command],cwd=out/f'{stage}-tree/src/wago',stdout=log,stderr=subprocess.STDOUT,check=True)
  finally:stop.set();thread.join()
BENCH
