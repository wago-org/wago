#!/usr/bin/env bash
set -euo pipefail
case $(uname -m) in aarch64|arm64|x86_64) ;; *) echo 'Native AMD64 or ARM64 required' >&2; exit 2;; esac
test "$(go env GOARCH)" = "$(go env GOHOSTARCH)"
repo=$(git rev-parse --show-toplevel)
base=ed76fd17957fd472b92a69d2fa3f5f63680f2cd5
head=5a7ca4f9e1e60a61eb4c8b55341008efc77cbd14
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
for file in src/wago/shared_grow_high_delta_test.go src/wago/shared_grow_high_delta_profile_test.go; do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
done
git -C "$out/base-tree" add -N src/wago/shared_grow_high_delta_test.go src/wago/shared_grow_high_delta_profile_test.go
cat > "$out/highdelta_evidence_test.go" <<'DUMPHARNESS'
//go:build (linux || darwin) && (amd64 || arm64) && !tinygo
package wago
import("testing";"os";"fmt";"path/filepath")
func TestHighDeltaDump(t *testing.T){dir:=os.Getenv("HIGHD_DUMP_DIR");if dir==""{t.Skip("evidence only")};for _,index:=range []byte{0,1}{c,err:=sharedGrowHighDeltaConfig().Compile(sharedGrowHighDeltaModule(index,false));if err!=nil{t.Fatal(err)};if err:=os.WriteFile(filepath.Join(dir,fmt.Sprintf("index-%d.bin",index)),c.code,0600);err!=nil{t.Fatal(err)};t.Logf("index%d native bytes%d",index,len(c.code));c.Close()}}
DUMPHARNESS
for stage in base head; do
 cp "$out/highdelta_evidence_test.go" "$out/$stage-tree/src/wago/highdelta_evidence_test.go"
 (
  cd "$out/$stage-tree"
  git add -N src/wago/highdelta_evidence_test.go
  git diff --binary > "$out/$stage-test-overlay.diff"
  sha256sum src/wago/shared_grow_high_delta_test.go src/wago/shared_grow_high_delta_profile_test.go src/wago/highdelta_evidence_test.go > "$out/$stage-harness-sha256.txt"
  git rev-parse HEAD > "$out/$stage-commit.txt"
  python3 "$out/campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago
  python3 "$out/campaign/run.py" build go test -p 4 -c -tags=wago_profile,wago_regalloccheck -o "$out/$stage-profile.test" ./src/wago
  go tool objdump -s 'wago.*dispatchAtomicWaitHelperContext$' "$out/$stage.test" > "$out/$stage-helper.asm"
  go tool objdump -s 'wago.*currentPages$' "$out/$stage.test" > "$out/$stage-currentPages.asm"
  go tool nm -size "$out/$stage.test" > "$out/$stage-nm.txt"
  cd src/wago
  set +e
  python3 "$out/campaign/run.py" build "$out/$stage.test" -test.run '^TestSharedGrowHighDeltaSelectedRead$' -test.count=1 -test.v -test.timeout=120s > "$out/$stage-ordering.txt" 2>&1
  ordering_status=$?
  set -e
  printf '%s\n' "$ordering_status" > "$out/$stage-ordering.exit"
  python3 - "$out" "$stage" "$ordering_status" <<'ORDERING'
import sys,pathlib,re
r=pathlib.Path(sys.argv[1]);stage=sys.argv[2];status=int(sys.argv[3]);s=(r/f'{stage}-ordering.txt').read_text()
assert 'panic:' not in s and 'timed out' not in s and 'did not complete' not in s
failed=re.findall(r'^\s*--- FAIL: (TestSharedGrowHighDeltaSelectedRead/[^\n]+) \(',s,re.M)
passed=re.findall(r'^\s*--- PASS: (TestSharedGrowHighDeltaSelectedRead/[^\n]+) \(',s,re.M)
if stage=='base':
 assert status==1 and len(failed)==24 and len(passed)==12,(status,len(failed),len(passed))
 assert all(int(x.rsplit('/delta',1)[1],16)>0xffffffff for x in failed)
 assert s.count('grow returned before the selected memory read guard was released:')==24
else:assert status==0 and len(failed)==0 and len(passed)==36,(status,len(failed),len(passed))
ORDERING
  python3 "$out/campaign/run.py" build env HIGHD_DUMP_DIR="$out/$stage-code" "$out/$stage.test" -test.run '^(TestHighDeltaDump|TestSharedGrowHighDeltaResults)$' -test.v -test.count=1 > "$out/$stage-correctness.txt" 2>&1
  python3 "$out/campaign/run.py" build "$out/$stage-profile.test" -test.run '^TestSharedGrowHighDeltaHelperEntries$' -test.v -test.count=1 > "$out/$stage-helpers.txt" 2>&1
 )
done
for index in 0 1;do cmp "$out/base-code/index-$index.bin" "$out/head-code/index-$index.bin";done
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
    commands=[['-test.bench=^BenchmarkSharedGrowHighDelta(Compile)?$','-test.benchtime=200ms'],['-test.bench=^BenchmarkInvokeHostFuncDirect$','-test.benchtime=200ms'],['-test.bench=^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host(0|1)$/^n(0|64|1024)$','-test.benchtime=200ms']]
    for command in commands:
     subprocess.run([str(out/f'{stage}.test'),'-test.run=^$','-test.benchmem','-test.count=1',*command],cwd=out/f'{stage}-tree/src/wago',stdout=log,stderr=subprocess.STDOUT,check=True)
  finally:stop.set();thread.join()
BENCH
