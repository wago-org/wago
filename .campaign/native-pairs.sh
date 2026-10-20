#!/usr/bin/env bash
set -euo pipefail
# Usage: native-pairs-21fb52e9.sh REPO BASE HEAD OUT CAMPAIGN
# Both AMD64 and ARM64 are native-only; this script does not use QEMU.
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
for file in src/wago/instruction_tail_native_test.go src/wago/instruction_tail_bench_test.go;do
 git -C "$repo" show "$head:$file" > "$out/base-tree/$file"
 for stage in base head;do sha256sum "$out/$stage-tree/$file" >> "$out/overlay-sha256.txt";done
 git -C "$out/base-tree" add -N -- "$file"
done
cat > "$out/tail-code-probe_test.go" <<'GO'
package wago
import("context";"crypto/sha256";"os";"path/filepath";"testing")
func TestCampaignTailCodeProbe(t *testing.T) {
 for _,mode:=range []string{"fast","fallback","mixed"} {
  var calls uint64
  rt:=checksumRuntime(t,&calls)
  mod,err:=rt.Compile(instructionTailChecksumModule(mode));if err!=nil{t.Fatal(err)}
  in,err:=rt.Instantiate(context.Background(),mod);if err!=nil{t.Fatal(err)}
  got,err:=in.Invoke("run",I32(256),I32(123));if err!=nil||len(got)!=1{t.Fatalf("%s: %v,%v",mode,got,err)}
  want:=uint32(123);for n:=uint32(256);n>0;n--{want=((want+n)^0x5a)&255};if uint32(got[0])!=want{t.Fatal("checksum mismatch")}
  code:=mod.Compiled().code
  root:=os.Getenv("WAGO_TAIL_CODE_DIR");if err:=os.MkdirAll(root,0755);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(root,mode+".bin"),code,0644);err!=nil{t.Fatal(err)}
  t.Logf("%s: operations=256 helpers=%d code-B=%d code-sha256=%x",mode,calls,len(code),sha256.Sum256(code))
  in.Close();mod.Close()
 }
}
GO
for stage in base head;do
 cp "$out/tail-code-probe_test.go" "$out/$stage-tree/src/wago/campaign_tail_code_probe_test.go"
 sha256sum "$out/$stage-tree/src/wago/campaign_tail_code_probe_test.go" >> "$out/overlay-sha256.txt"
 git -C "$out/$stage-tree" add -N -- src/wago/campaign_tail_code_probe_test.go
 git -C "$out/$stage-tree" diff --binary > "$out/$stage-test-overlay.diff"
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago
 )
done
(
 cd "$out/head-tree"
 python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck -o "$out/head-debug.test" ./src/wago
)
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" > "$out/binary-sha256.txt"
(
 cd "$out/base-tree/src/wago"
 python3 "$campaign/run.py" build env WAGO_TAIL_CODE_DIR="$out/base-code" "$out/base.test" -test.run '^(TestCampaignTailCodeProbe|TestCustomImportedTailChecksumLoop|TestCustomImportedTailPreservesExactImportIdentity)$' -test.count=1 -test.v > "$out/base-oracle.txt" 2>&1
)
for binary in head head-debug;do
 (
  cd "$out/head-tree/src/wago"
  python3 "$campaign/run.py" build env WAGO_TAIL_CODE_DIR="$out/$binary-code" "$out/$binary.test" -test.run 'TestCampaignTailCodeProbe|TestCustomImportedTail|TestTailCallsPreservePendingTraps|TestCustomInstruction' -test.count=1 -test.v > "$out/$binary-correctness.txt" 2>&1
 )
done
python3 "$campaign/run.py" bench env GOMAXPROCS=4 bash -s -- "$out" <<'BENCH'
set -euo pipefail
out=$1
for pair in 1 2 3 4 5 6 7;do
 order='base head';if (( pair%2==0 ));then order='head base';fi
 for stage in $order;do
  (
   cd "$out/$stage-tree/src/wago"
   "$out/$stage.test" -test.run '^$' -test.bench '^(BenchmarkInstructionTail(Invoke|Compile|BudgetCompile)|BenchmarkInvokeHostFuncDirect)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 > "$out/pair-$pair-$stage.txt" 2>&1
   "$out/$stage.test" -test.run '^$' -test.bench '^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host[01]$/^n(0|64|1024)$' -test.benchmem -test.benchtime=250ms -test.count=1 -test.cpu=4 >> "$out/pair-$pair-$stage.txt" 2>&1
  )
 done
done
BENCH
