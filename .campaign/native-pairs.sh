#!/usr/bin/env bash
set -euo pipefail
case $(uname -m) in aarch64|arm64|x86_64) ;; *) echo 'Native AMD64 or ARM64 required' >&2; exit 2;; esac
test "$(go env GOARCH)" = "$(go env GOHOSTARCH)"
repo=$(git rev-parse --show-toplevel)
base=7997c86c36e05cfba4c4f39cabb026a318f55fc0
head=921ce4c8ecf7f73afb28fcab43b2c6f7f0842ff8
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
for file in src/wago/shared_grow_failure_test.go src/wago/shared_grow_failure_profile_test.go; do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
done
git -C "$out/base-tree" add -N src/wago/shared_grow_failure_test.go src/wago/shared_grow_failure_profile_test.go
# The baseline intentionally expects every grow to enter Go.
python3 - "$out/base-tree/src/wago/shared_grow_failure_profile_test.go" <<'PYBASE'
import sys,pathlib
p=pathlib.Path(sys.argv[1]);s=p.read_text().replace('{"constant-fail", 0, 0}', '{"constant-fail", 0, iterations}').replace('{"mixed", 2, iterations / 2}', '{"mixed", 2, iterations}')
p.write_text(s)
PYBASE
cat > "$out/mem02_evidence_test.go" <<'DUMPHARNESS'
//go:build (linux || darwin) && (amd64 || arm64) && !tinygo
package wago
import("testing"; "os"; "fmt"; "path/filepath")
func TestMem02Dump(t *testing.T) {
 dir:=os.Getenv("MEM02_DUMP_DIR"); if dir=="" {t.Skip("evidence only")}
 for _, wide:=range []bool{false,true} {for _, index:=range []byte{0,1} {
 c,err:=sharedGrowFailureConfig().Compile(sharedGrowFailureModule(wide,index,1,1,1));if err!=nil{t.Fatal(err)}
 path:=filepath.Join(dir,fmt.Sprintf("i64-%t-index-%d.bin",wide,index));if err:=os.WriteFile(path,c.code,0600);err!=nil{t.Fatal(err)}
 t.Logf("%s %d native bytes",path,len(c.code)); c.Close()
 }}
}
DUMPHARNESS
for stage in base head; do
 cp "$out/mem02_evidence_test.go" "$out/$stage-tree/src/wago/mem02_evidence_test.go"
 (
  cd "$out/$stage-tree"
  git add -N src/wago/mem02_evidence_test.go
  git diff --binary > "$out/$stage-test-overlay.diff"
  sha256sum src/wago/shared_grow_failure_test.go src/wago/shared_grow_failure_profile_test.go src/wago/mem02_evidence_test.go > "$out/$stage-harness-sha256.txt"
  git rev-parse HEAD > "$out/$stage-commit.txt"
  python3 "$out/campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago
  python3 "$out/campaign/run.py" build go test -p 4 -c -tags=wago_profile,wago_regalloccheck -o "$out/$stage-profile.test" ./src/wago
  cd src/wago
  python3 "$out/campaign/run.py" build env MEM02_DUMP_DIR="$out/$stage-code" "$out/$stage.test" -test.run '^(TestMem02Dump|TestSharedGrowImpossible.*|TestThreadsSharedMemoryConcurrentGrowth|TestThreadsSharedMemoryGrowToMemory32Limit)$' -test.v -test.count=1 > "$out/$stage-correctness.txt" 2>&1
  python3 "$out/campaign/run.py" build "$out/$stage-profile.test" -test.run '^TestSharedGrowImpossible' -test.v -test.count=1 > "$out/$stage-helpers.txt" 2>&1
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
    commands=[['-test.bench=^BenchmarkSharedGrowImpossible(Loop|Compile)$','-test.benchtime=200ms'],['-test.bench=^BenchmarkInvokeHostFuncDirect$','-test.benchtime=200ms'],['-test.bench=^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host(0|1)$/^n(0|64|1024)$','-test.benchtime=200ms']]
    for command in commands:
     subprocess.run([str(out/f'{stage}.test'),'-test.run=^$','-test.benchmem','-test.count=1',*command],cwd=out/f'{stage}-tree/src/wago',stdout=log,stderr=subprocess.STDOUT,check=True)
  finally:stop.set();thread.join()
BENCH
