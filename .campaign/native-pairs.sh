#!/usr/bin/env bash
set -euo pipefail
repo=$(realpath "${1:-$PWD}")
base=${2:-7997c86c36e05cfba4c4f39cabb026a318f55fc0}
head=${3:-8edac8455ea6777847d648e49bd9eeaeb6231017}
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
mkdir -p "$out/baseline-harness"
for file in src/wago/gc_native_null_extern_test.go src/wago/gc_native_null_extern_stats_amd64_test.go src/wago/gc_native_null_extern_stats_arm64_test.go; do
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
( cd "$out/base-tree/src/wago"; python3 "$campaign/run.py" build "$out/base.test" -test.run '^TestGCNativeNullExtern|^TestGCExternConversion|^TestGCDropped' -test.v > "$out/base-oracle.txt" 2>&1 )
for stage in head head-debug; do
 ( cd "$out/head-tree/src/wago"; python3 "$campaign/run.py" build "$out/$stage.test" -test.run '^TestGCNativeNullExtern|^TestGCExternConversion|^TestGCDropped' -test.v > "$out/$stage-correctness.txt" 2>&1 )
done
cat > "$out/probe-counter.go" <<'PROBE_SOURCE'
package wago
var campaignNullExternEntries uint64

PROBE_SOURCE
cat > "$out/probe_test.go" <<'PROBE_SOURCE'
package wago

import (
 "crypto/sha256"
 "fmt"
 "os"
 "path/filepath"
 "testing"
)

func TestCampaignNullExternProbe(t *testing.T) {
 root:=os.Getenv("CAMPAIGN_NULL_EXTERN_DIR");if err:=os.MkdirAll(root,0755);err!=nil {t.Fatal(err)}
 for _,op:=range []byte{26,27} {
  c,err:=Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3),gcNativeNullExternLoop(op));if err!=nil {t.Fatal(err)}
  in,err:=Instantiate(c,InstantiateOptions{});if err!=nil {t.Fatal(err)}
  foreign,err:=in.NewExternRef("probe");if err!=nil {t.Fatal(err)}
  for _,path:=range []string{"null","fallback","mixed"} {
   word,mask:=uint64(0),uint64(0);if path!="null" {word=ValueExternRef(foreign).Bits()};if path=="mixed" {mask=1}
   campaignNullExternEntries=0
   if _,err:=in.Invoke("run",1000,word,mask);err!=nil {t.Fatal(err)}
   t.Logf("op%d/%s helpers=%d operations=1000 native_bytes=%d code_sha256=%x",op,path,campaignNullExternEntries,len(c.code),sha256.Sum256(c.code))
  }
  if err:=os.WriteFile(filepath.Join(root,fmt.Sprintf("op%d.bin",op)),c.code,0600);err!=nil {t.Fatal(err)}
  in.Close();c.Close()
 }
 c,err:=Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3),gcNativeNullExternModule());if err!=nil {t.Fatal(err)}
 in,err:=Instantiate(c,InstantiateOptions{});if err!=nil {t.Fatal(err)}
 for _,name:=range []string{"internalize-static","externalize-static"} {
  campaignNullExternEntries=0
  if _,err:=in.Invoke(name);err!=nil {t.Fatal(err)}
  t.Logf("%s helpers=%d operations=1",name,campaignNullExternEntries)
 }
 in.Close();c.Close()
}

PROBE_SOURCE
for stage in base head; do
 python3 - "$out/$stage-tree" "$out" <<'PY_OVERLAY'
import pathlib,sys,json
src=pathlib.Path(sys.argv[1]); out=pathlib.Path(sys.argv[2]); p=src/'src/wago/gc_struct_helper.go'
s=p.read_text();needle='func (in *Instance) dispatchGCStructHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) {'
assert needle in s;s=s.replace(needle,needle+'\n\tif helper == gcAnyConvertExtern || helper == gcExternConvertAny { campaignNullExternEntries++ }',1)
saved=out/(src.name+'-instrumented-helper.go');saved.write_text(s)
mapping={str(p):str(saved),str(src/'src/wago/campaign_null_extern_counter.go'):str(out/'probe-counter.go'),str(src/'src/wago/campaign_null_extern_probe_test.go'):str(out/'probe_test.go')}
(out/(src.name+'-probe-overlay.json')).write_text(json.dumps({'Replace':mapping}))
PY_OVERLAY
 ( cd "$out/$stage-tree"; python3 "$campaign/run.py" build go test -p 4 -overlay="$out/$stage-tree-probe-overlay.json" -c -o "$out/$stage-probe.test" ./src/wago )
 ( cd "$out/$stage-tree/src/wago"; python3 "$campaign/run.py" build env CAMPAIGN_NULL_EXTERN_DIR="$out/$stage-code" "$out/$stage-probe.test" -test.run '^TestCampaignNullExternProbe$' -test.v > "$out/$stage-helper-code-probe.txt" 2>&1 )
done
python3 "$campaign/run.py" bench python3 - "$out" <<'PY_BENCH'
import os,subprocess,pathlib,sys,time,json
r=pathlib.Path(sys.argv[1]); allowed=sorted(os.sched_getaffinity(0)); os.sched_setaffinity(0,{allowed[0]}); (r/'cpu-affinity.txt').write_text(str(allowed[0]))
pattern='^(BenchmarkGCNativeNullExtern(|Compile)|BenchmarkGCInstructionLoopControl|BenchmarkInvokeHostFuncDirect)$'
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
