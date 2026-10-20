#!/usr/bin/env bash
set -euo pipefail
# Usage: native-pairs-bac33496.sh REPO BASE HEAD OUT CAMPAIGN
repo=$(realpath "$1")
base=$2
head=$3
out=$(realpath -m "$4")
campaign=$(realpath "$5")
mkdir -p "$out"
case "$(uname -m)/$(go env GOARCH)" in x86_64/amd64) ;; *) echo 'Native AMD64 required' >&2;exit 2;;esac
export GOMAXPROCS=4
{
 date -u
 uname -a
 go version
 go env GOOS GOARCH
 lscpu
 printf 'baseline=%s\ncandidate=%s\nGOMAXPROCS=4\n' "$base" "$head"
} > "$out/environment.txt"
base_added=0
head_added=0
cleanup() {
 if (( base_added ));then git -C "$repo" worktree remove --force "$out/base-tree";fi
 if (( head_added ));then git -C "$repo" worktree remove --force "$out/head-tree";fi
}
trap cleanup EXIT
git -C "$repo" worktree add --detach "$out/base-tree" "$base"
base_added=1
git -C "$repo" worktree add --detach "$out/head-tree" "$head"
head_added=1
file=src/wago/gc_default_struct_native_amd64_test.go
git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
git -C "$out/base-tree" add -N -- "$file"
for stage in base head;do
 sha256sum "$out/$stage-tree/$file" >> "$out/overlay-sha256.txt"
 git -C "$out/$stage-tree" diff --binary > "$out/$stage-test-overlay.diff"
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago
 )
done
cat > "$out/probe_test.go" <<'GO'
package wago
import("testing";"os";"path/filepath";"crypto/sha256")
func TestCampaignDefaultAllocProbe(t *testing.T){
 for _,mode:=range []string{"fast","fallback","mixed"}{
  c,err:=Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3),gcDefaultStructLoopBytes(true,4));if err!=nil{t.Fatal(err)}
  cfg:=GCConfig{Profile:GCProfileThroughput};if mode=="fallback"{cfg.Profile=GCProfileTiny}else if mode=="mixed"{cfg.StressNurseryBytes=256}
  in,err:=Instantiate(c,InstantiateOptions{GC:cfg});if err!=nil{t.Fatal(err)}
  campaignDefaultAllocHelpers=0
  if _,err:=in.Invoke("run",256);err!=nil{t.Fatal(err)}
  t.Logf("%s: cold-operations=256 cold-helpers=%d",mode,campaignDefaultAllocHelpers)
  before:=in.gc.Stats().Allocations;campaignDefaultAllocHelpers=0
  got,err:=in.Invoke("run",256);if err!=nil||len(got)!=1||got[0]!=0{t.Fatalf("%s: %v,%v",mode,got,err)}
  t.Logf("%s: operations=256 helpers=%d semantic-allocations=%d code-B=%d code-sha256=%x",mode,campaignDefaultAllocHelpers,in.gc.Stats().Allocations-before,c.CodeSize(),sha256.Sum256(c.code))
  root:=os.Getenv("CAMPAIGN_DEFAULT_CODE_DIR");if err:=os.MkdirAll(root,0755);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(root,mode+".bin"),c.code,0644);err!=nil{t.Fatal(err)}
  in.Close();c.Close()
 }
}
GO
printf 'package wago\nvar campaignDefaultAllocHelpers uint64\n' > "$out/counter.go"
for stage in base head;do
 python3 - "$out" "$stage" <<'PYOVERLAY'
from pathlib import Path
import sys,json
out=Path(sys.argv[1]);stage=sys.argv[2];tree=out/(stage+'-tree')
source=tree/'src/wago/gc_struct_helper.go'
text=source.read_text()
marker='func (in *Instance) dispatchGCStructHelperParked(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) {\n'
assert text.count(marker)==1
instrumented=out/(stage+'-instrumented-helper.go')
instrumented.write_text(text.replace(marker,marker+'\tif helper == gcStructAllocDefault || helper == gcStructAllocOne { campaignDefaultAllocHelpers++ }\n'))
replace={str(source):str(instrumented),str(tree/'src/wago/campaign_default_counter.go'):str(out/'counter.go'),str(tree/'src/wago/campaign_default_probe_test.go'):str(out/'probe_test.go')}
(out/(stage+'-probe-overlay.json')).write_text(json.dumps({'Replace':replace},indent=2))
PYOVERLAY
 sha256sum "$out/$stage-instrumented-helper.go" "$out/$stage-probe-overlay.json" "$out/probe_test.go" "$out/counter.go" >> "$out/probe-input-sha256.txt"
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -overlay "$out/$stage-probe-overlay.json" -c -o "$out/$stage-probe.test" ./src/wago
  cd src/wago
  python3 "$campaign/run.py" build env CAMPAIGN_DEFAULT_CODE_DIR="$out/$stage-code" "$out/$stage-probe.test" -test.run '^TestCampaignDefaultAllocProbe$' -test.count=1 -test.v > "$out/$stage-probe.txt" 2>&1
 )
done
(
 cd "$out/base-tree/src/wago"
 python3 "$campaign/run.py" build "$out/base.test" -test.run '^TestGCDefaultStruct(FallbackAndCollection|InterruptAndRecovery)$' -test.count=1 -test.v > "$out/base-oracle.txt" 2>&1
)
(
 cd "$out/head-tree"
 python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck -o "$out/head-debug.test" ./src/wago
 cd src/wago
 python3 "$campaign/run.py" build "$out/head-debug.test" -test.run 'TestGCDefaultStruct|TestGCNativeStruct|TestGCStructNative|TestGCNativeArrayAllocTrapCancelsBatch' -test.count=1 -test.v > "$out/head-debug-correctness.txt" 2>&1
)
sha256sum "$out/"*.test > "$out/binary-sha256.txt"
python3 "$campaign/run.py" bench env GOMAXPROCS=4 bash -s -- "$out" <<'BENCH'
set -euo pipefail
out=$1
for pair in 1 2 3 4 5 6 7;do
 order='base head';if (( pair%2==0 ));then order='head base';fi
 for stage in $order;do
  (
   cd "$out/$stage-tree/src/wago"
   "$out/$stage.test" -test.run '^$' -test.bench '^(BenchmarkGCDefaultStruct(Invoke|Compile)|BenchmarkInvokeHostFuncDirect)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 > "$out/pair-$pair-$stage.txt" 2>&1
   "$out/$stage.test" -test.run '^$' -test.bench '^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host[01]$/^n(0|64|1024)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 >> "$out/pair-$pair-$stage.txt" 2>&1
  )
 done
done
BENCH
