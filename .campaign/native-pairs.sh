#!/usr/bin/env bash
set -euo pipefail
repo=$(realpath "${1:-$PWD}")
base=${2:-7997c86c36e05cfba4c4f39cabb026a318f55fc0}
head=${3:-9fe81ea747a852cb165d16685181d148249011f2}
out=$(realpath -m "${4:-${RUNNER_TEMP:-/tmp}/specialization-evidence}")
campaign=$(realpath "${5:-$repo/.campaign}")
mkdir -p "$out" "$campaign/locks"
case $(uname -m) in aarch64|arm64|x86_64) ;; *) echo 'Native AMD64 or ARM64 required' >&2; exit 2;; esac
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
# Match the Linux AMD64 CI tool and source contract; do not skip corpus tests.
revision=9d36019973201a19f9c9ebb0f10828b2fe2374aa
(
 cd "$repo"
 git submodule update --init --depth=1 tests/conformance/spec-v3
 test "$(git -C tests/conformance/spec-v3 rev-parse HEAD)" = "$revision"
 sudo apt-get update
 sudo apt-get install -y ocaml-nox ocaml-dune menhir
 scripts/bootstrap-wabt.sh --verify
 scripts/bootstrap-spec-interpreter.sh --verify
) > "$out/spec-tool-setup.txt" 2>&1
export PATH="$(dirname "$("$repo/scripts/bootstrap-wabt.sh" --print-path)"):$PATH"
export WAGO_SPEC_INTERPRETER="$("$repo/scripts/bootstrap-spec-interpreter.sh" --print-path)"
export WAGO_SPEC_INTERPRETER_REVISION="$revision"
sha256sum "$WAGO_SPEC_INTERPRETER" "$(command -v wast2json)" > "$out/spec-tool-sha256.txt"
cp "$(dirname "$WAGO_SPEC_INTERPRETER")/build-provenance" "$out/spec-interpreter-build-provenance.txt"
for stage in base head; do
 git clone --shared "$repo/tests/conformance/spec-v3" "$out/$stage-tree/tests/conformance/spec-v3" >> "$out/spec-tool-setup.txt" 2>&1
 test "$(git -C "$out/$stage-tree/tests/conformance/spec-v3" rev-parse HEAD)" = "$revision"
done
mkdir -p "$out/baseline-harness"
for file in src/wago/gc_native_i31_none_test.go src/wago/gc_native_i31_none_stats_amd64_test.go src/wago/gc_native_i31_none_stats_arm64_test.go; do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
 cp "$out/base-tree/$file" "$out/baseline-harness/$(basename "$file")"
done
sha256sum "$out"/baseline-harness/* > "$out/baseline-harness-sha256.txt"
for file in "$out"/baseline-harness/*; do git diff --no-index -- /dev/null "$file" >> "$out/baseline-test-overlay.diff" || test "$?" -eq 1; done
for stage in base head; do
 ( cd "$out/$stage-tree"; python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago )
done
( cd "$out/head-tree"; python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck,wago_codegenstats -o "$out/head-debug.test" ./src/wago )
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" > "$out/binary-sha256.txt"
( cd "$out/base-tree/src/wago"; python3 "$campaign/run.py" build "$out/base.test" -test.run '^TestGCNativeI31None' -test.v > "$out/base-oracle.txt" 2>&1 )
for stage in head head-debug; do
 ( cd "$out/head-tree/src/wago"; python3 "$campaign/run.py" build "$out/$stage.test" -test.run '^TestGCNativeI31None|^TestGC.*(I31|Reference|Defined)|^TestStagedGCRef' -test.v > "$out/$stage-correctness.txt" 2>&1 )
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
python3 "$campaign/run.py" bench python3 - "$out" <<'PY_BENCH'
import os,subprocess,pathlib,sys,time,json
r=pathlib.Path(sys.argv[1]); allowed=sorted(os.sched_getaffinity(0)); os.sched_setaffinity(0,{allowed[0]}); (r/'cpu-affinity.txt').write_text(str(allowed[0]))
pattern='^(BenchmarkGCNativeI31None(|Compile|Object)|BenchmarkGCInstructionLoopControl|BenchmarkInvokeHostFuncDirect)$'
def external_go():
 return [line for line in subprocess.check_output(['ps','-eo','comm,args'],text=True).splitlines() if line.split(None,1)[0] in ('go','compile','link')]
for pair in range(7):
 attempt=0
 while True:
  while external_go(): time.sleep(1)
  before=external_go(); attempt+=1
  for stage in (['base','head'] if pair%2==0 else ['head','base']):
   p=subprocess.run([str(r/(stage+'.test')),'-test.run=^$','-test.bench='+pattern,'-test.benchmem','-test.benchtime=200ms','-test.count=1'],cwd=str(r/(stage+'-tree')/'src/wago'),text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (r/(stage+'-sample'+str(pair)+'.txt')).write_text(p.stdout)
   if p.returncode: raise SystemExit(p.returncode)
  after=external_go()
  (r/('process-pair'+str(pair)+'-attempt'+str(attempt)+'.json')).write_text(json.dumps({'before':before,'after':after}))
  if not before and not after: break
  for stage in ['base','head']:
   (r/(stage+'-sample'+str(pair)+'.txt')).rename(r/(stage+'-sample'+str(pair)+'-contaminated'+str(attempt)+'.txt'))
  if attempt>=10: raise SystemExit('repeated external build contamination')

PY_BENCH
