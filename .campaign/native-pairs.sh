#!/usr/bin/env bash
set -euo pipefail
repo=$(git rev-parse --show-toplevel)
base=ed76fd17957fd472b92a69d2fa3f5f63680f2cd5
head=243032c07eb23a2a09e620f168b585c20f9266ad
out="${RUNNER_TEMP:?}/specialization-evidence"
campaign="$repo/.campaign"
mkdir -p "$out/base-code" "$out/head-code"
test "$(uname -m)" = x86_64
test "$(go env GOHOSTARCH)" = amd64
export GOMAXPROCS=4
{ date -u; uname -a; go version; lscpu; printf 'base=%s\nhead=%s\n' "$base" "$head"; } > "$out/environment.txt"
base_added=0
head_added=0
cleanup(){ if ((base_added)); then git -C "$repo" worktree remove --force "$out/base-tree"; fi; if ((head_added)); then git -C "$repo" worktree remove --force "$out/head-tree"; fi; }
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
for file in src/wago/gc_elem_drop_native_test.go src/wago/gc_elem_drop_native_profile_test.go; do git -C "$repo" show "$head:$file" > "$out/base-tree/$file"; done
python3 - "$out/base-tree/src/wago/gc_elem_drop_native_profile_test.go" <<'PY'
from pathlib import Path
import sys
p=Path(sys.argv[1]);s=p.read_text();assert 'want := 0' in s;s=s.replace('want := 0','want := iterations').replace('want = iterations / 2','want = iterations');p.write_text(s)
PY
cat > "$out/elem_drop_evidence_test.go" <<'GO'
//go:build linux && amd64 && !tinygo
package wago
import("testing";"os";"crypto/sha256")
func TestCampaignElemDropCode(t *testing.T){c,err:=Compile(gcElemDropConfig(),gcElemDropModule(false));if err!=nil{t.Fatal(err)};defer c.Close();t.Logf("native_bytes=%d code_sha256=%x",len(c.code),sha256.Sum256(c.code));if err:=os.WriteFile(os.Getenv("ELEM_DROP_CODE_FILE"),c.code,0600);err!=nil{t.Fatal(err)}}
GO
for stage in base head; do
 cp "$out/elem_drop_evidence_test.go" "$out/$stage-tree/src/wago/elem_drop_evidence_test.go"
 (
 cd "$out/$stage-tree"
 git add -N src/wago/gc_elem_drop_native_test.go src/wago/gc_elem_drop_native_profile_test.go src/wago/elem_drop_evidence_test.go
 git diff --binary > "$out/$stage-test-overlay.diff"
 git rev-parse HEAD > "$out/$stage-commit.txt"
 sha256sum src/wago/gc_elem_drop_native_test.go src/wago/gc_elem_drop_native_profile_test.go src/wago/elem_drop_evidence_test.go > "$out/$stage-harness-sha256.txt"
 python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago
 python3 "$campaign/run.py" build go test -p 4 -tags=wago_profile,wago_regalloccheck -c -o "$out/$stage-profile.test" ./src/wago
 cd src/wago
 python3 "$campaign/run.py" build "$out/$stage.test" -test.run '^TestGCNonrootElemDrop|^TestStagedGCArray|^TestGenericGCArrayNewElem|^TestTableInitUsesOriginalElementIndexAndElemDrop|^TestElemDropAfterPassiveNullExpressionSegment' -test.v > "$out/$stage-correctness.txt" 2>&1
 python3 "$campaign/run.py" build "$out/$stage-profile.test" -test.run '^TestGCNonrootElemDrop' -test.v > "$out/$stage-helper-counts.txt" 2>&1
 python3 "$campaign/run.py" build env ELEM_DROP_CODE_FILE="$out/$stage-code/module.bin" "$out/$stage.test" -test.run '^TestCampaignElemDropCode$' -test.v > "$out/$stage-code-probe.txt" 2>&1
 )
done
sha256sum "$out/"*.test > "$out/binary-sha256.txt"
sha256sum "$out/"*-test-overlay.diff "$repo/.campaign/native-pairs.sh" > "$out/build-input-sha256.txt"
python3 "$campaign/run.py" bench python3 - "$out" <<'PY'
from pathlib import Path
import sys,os,subprocess,json,time
r=Path(sys.argv[1]);allowed=sorted(os.sched_getaffinity(0));os.sched_setaffinity(0,{allowed[0]});(r/'affinity.json').write_text(json.dumps({'available':allowed,'used':sorted(os.sched_getaffinity(0))}))
def builds():return [s for s in subprocess.check_output(['ps','-eo','comm,args'],text=True).splitlines() if s.split(None,1)[0] in ['go','compile','link']]
for pair in range(7):
 attempt=0
 while True:
  while builds():time.sleep(1)
  before=builds();attempt+=1
  for stage in (['base','head'] if pair%2==0 else ['head','base']):
   p=subprocess.run([str(r/(stage+'.test')),'-test.run=^$','-test.bench=^(BenchmarkGCNonrootElemDrop(|Compile)|BenchmarkInvokeHostFuncDirect)$','-test.benchmem','-test.benchtime=200ms','-test.count=1'],cwd=r/(stage+'-tree')/'src/wago',text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (r/f'{stage}-sample{pair}.txt').write_text(p.stdout)
   if p.returncode:raise SystemExit(p.returncode)
  after=builds();(r/f'process-pair{pair}-attempt{attempt}.json').write_text(json.dumps({'before':before,'after':after}))
  if not before and not after:break
  for stage in ['base','head']:(r/f'{stage}-sample{pair}.txt').rename(r/f'{stage}-sample{pair}-contaminated{attempt}.txt')
  if attempt>=10:raise SystemExit('repeated external build contamination')
PY
