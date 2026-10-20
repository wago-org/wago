#!/usr/bin/env bash
set -euo pipefail
# Usage: native-pairs-a91b328e.sh REPO BASE HEAD OUT CAMPAIGN
repo=$(realpath "$1")
base=$2
head=$3
out=$(realpath -m "$4")
campaign=$(realpath "$5")
mkdir -p "$out"
case "$(uname -m)/$(go env GOARCH)" in x86_64/amd64|aarch64/arm64|arm64/arm64) ;; *) echo 'Matching native AMD64 or ARM64 required' >&2;exit 2;;esac
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
for file in src/core/plugins/instruction_stack_classifier_test.go src/wago/instruction_classifier_test.go;do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
 git -C "$out/base-tree" add -N -- "$file"
 for stage in base head;do sha256sum "$out/$stage-tree/$file" >> "$out/overlay-sha256.txt";done
done
cat > "$out/classifier-code-probe_test.go" <<'GO'
package wago
import("context";"crypto/sha256";"os";"path/filepath";"testing")
func TestCampaignClassifierCodeProbe(t *testing.T) {
 for _,shape:=range []string{"normal","shared","deep","unreachable"} {
  var calls uint64
  ext:=classifierExecutionExtension(shape)
  eval:=ext.eval
  ext.eval=func(v []uint32)uint32{calls++;return eval(v)}
  rt:=NewRuntime()
  if err:=rt.Use(ext);err!=nil{t.Fatal(err)}
  mod,err:=rt.Compile(classifierExecutionModule());if err!=nil{t.Fatal(err)}
  in,err:=rt.Instantiate(context.Background(),mod);if err!=nil{t.Fatal(err)}
  args:=make([]uint64,17)
  for n:=uint32(0);n<256;n++ {
   args[0]=uint64(n)
   got,err:=in.Invoke("run",args...)
   want:=n+11;if shape=="normal"{want++};if shape=="shared"{want+=256}
   if err!=nil||len(got)!=1||uint32(got[0])!=want{t.Fatalf("%s input%d: %v,%v want%d",shape,n,got,err,want)}
  }
  code:=mod.Compiled().code
  root:=os.Getenv("WAGO_CLASSIFIER_CODE_DIR");if err:=os.MkdirAll(root,0755);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(root,shape+".bin"),code,0644);err!=nil{t.Fatal(err)}
  t.Logf("%s: operations=256 helpers=%d code-B=%d code-sha256=%x",shape,calls,len(code),sha256.Sum256(code))
  in.Close();mod.Close();rt.Close()
 }
}
GO
for stage in base head;do
 cp "$out/classifier-code-probe_test.go" "$out/$stage-tree/src/wago/campaign_classifier_code_probe_test.go"
 sha256sum "$out/$stage-tree/src/wago/campaign_classifier_code_probe_test.go" >> "$out/overlay-sha256.txt"
 git -C "$out/$stage-tree" add -N -- src/wago/campaign_classifier_code_probe_test.go
 git -C "$out/$stage-tree" diff --binary > "$out/$stage-test-overlay.diff"
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage-plugins.test" ./src/core/plugins
  python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage-wago.test" ./src/wago
 )
 (
  cd "$out/$stage-tree/src/wago"
  python3 "$campaign/run.py" build env WAGO_CLASSIFIER_CODE_DIR="$out/$stage-code" "$out/$stage-wago.test" -test.run 'TestCampaignClassifierCodeProbe|TestInstructionClassifier' -test.count=1 -test.v > "$out/$stage-oracle.txt" 2>&1
 )
done
(
 cd "$out/head-tree/src/core/plugins"
 python3 "$campaign/run.py" build "$out/head-plugins.test" -test.count=1 -test.v > "$out/head-plugins-correctness.txt" 2>&1
)
(
 cd "$out/head-tree"
 python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck -o "$out/head-debug.test" ./src/wago
 cd src/wago
 python3 "$campaign/run.py" build "$out/head-debug.test" -test.run 'TestInstructionClassifier|TestCustomInstruction|TestTailCallsPreservePendingTraps' -test.count=1 -test.v > "$out/head-debug-correctness.txt" 2>&1
)
sha256sum "$out/"*.test > "$out/binary-sha256.txt"
python3 "$campaign/run.py" bench env GOMAXPROCS=4 bash -s -- "$out" <<'BENCH'
set -euo pipefail
out=$1
for pair in 1 2 3 4 5 6 7;do
 order='base head';if (( pair%2==0 ));then order='head base';fi
 for stage in $order;do
  (
   cd "$out/$stage-tree/src/core/plugins"
   "$out/$stage-plugins.test" -test.run '^$' -test.bench '^BenchmarkInstruction(Preparation|NativeClassification)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 > "$out/pair-$pair-$stage.txt" 2>&1
   cd "$out/$stage-tree/src/wago"
   "$out/$stage-wago.test" -test.run '^$' -test.bench '^(BenchmarkInstructionClassifier(Compile|Invoke)|BenchmarkInvokeHostFuncDirect)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 >> "$out/pair-$pair-$stage.txt" 2>&1
   "$out/$stage-wago.test" -test.run '^$' -test.bench '^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host[01]$/^n(0|64|1024)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 >> "$out/pair-$pair-$stage.txt" 2>&1
  )
 done
done
BENCH
