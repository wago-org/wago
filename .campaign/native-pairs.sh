#!/usr/bin/env bash
set -euo pipefail
# Contract: REPO BASE HEAD OUT CAMPAIGN; native AMD64 or ARM64 only.
repo=$(realpath "$1")
base=$2
head=$3
out=$(realpath -m "$4")
campaign=$(realpath "$5")
mkdir -p "$out"
export GOMAXPROCS=4
arch=$(go env GOARCH)
case "$(uname -m):$arch" in aarch64:arm64) ;; *) echo 'Native ARM64 required' >&2; exit 2;; esac
cpu=$(python3 -c 'import os; a=sorted(os.sched_getaffinity(0));print(a[min(2,len(a)-1)])')
export WAGO_ARRAY_BENCH_CPU="$cpu"
{
 date -u
 uname -a
 go version
 go env GOOS GOARCH
 lscpu
 python3 -c 'import os;print("available_affinity="+str(sorted(os.sched_getaffinity(0))));print("benchmark_cpu="+os.environ["WAGO_ARRAY_BENCH_CPU"])'
 printf 'baseline=%s\ncandidate=%s\nGOMAXPROCS=4\n' "$base" "$head"
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
for harness in src/wago/gc_array_get_scalar_native_test.go src/wago/gc_array_get_scalar_guard_native_test.go src/wago/gc_array_get_scalar_extra_native_test.go src/wago/gc_array_get_scalar_pressure_native_test.go;do
 git -C "$repo" show "$head:$harness" > "$out/base-tree/$harness"
 sha256sum "$out/base-tree/$harness" "$out/head-tree/$harness" >> "$out/overlay-sha256.txt"
 git -C "$out/base-tree" add -N -- "$harness"
done
git -C "$out/base-tree" diff --binary > "$out/baseline-test-overlay.diff"
git -C "$repo" diff --binary "$base" "$head" > "$out/source.diff"
for stage in base head; do
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -c -o "$out/$stage.test" ./src/wago > "$out/$stage-build.txt" 2>&1
 )
done
(
 cd "$out/head-tree"
 python3 "$campaign/run.py" build go test -p 4 -c -tags=wago_regalloccheck,wago_codegenstats -o "$out/head-debug.test" ./src/wago > "$out/head-debug-build.txt" 2>&1
 python3 "$campaign/run.py" build go test -p 4 -tags=wago_regalloccheck,wago_codegenstats ./src/core/compiler/backend/railshot/"$arch" -run 'TestGCGeneralScalarArrayGetEligibility|TestGCGeneralArrayLen|TestGCObjectStub|TestArrayLenParameter|TestModuleGCResolverFamilyCounts|TestGCResolvedHandleReuse|TestPatchCallRelocs|TestCallRelocTable' -count=1 -v > "$out/backend-correctness.txt" 2>&1
)
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" > "$out/binary-sha256.txt"
for stage in base head head-debug; do
 tree=$stage
 if [[ $stage == head-debug ]];then tree=head;fi
 (cd "$out/$tree-tree/src/wago"; python3 "$campaign/run.py" build "$out/$stage.test" -test.run '^TestGCGeneralScalarArrayGet' -test.count=1 -test.v > "$out/$stage-correctness.txt" 2>&1)
done
# Counter builds use overlays. The normal timing binaries above stay unchanged.
for stage in base head;do
 python3 - "$stage" "$out/$stage-tree" "$out" <<'PY'
import pathlib,sys,json
stage,treepath,outpath=sys.argv[1:]
tree,out=pathlib.Path(treepath),pathlib.Path(outpath);out.mkdir(parents=True,exist_ok=True)
helper=tree/'src/wago/gc_array_helper.go';original=helper.read_text()
needle='case gcArrayGet, gcArrayGetS, gcArrayGetU:\n';assert original.count(needle)==1
hp=out/(stage+'-counter-helper.go');hp.write_text(original.replace('const (','var campaignArrayHelperEntries uint64\n\nconst (',1).replace(needle,needle+' campaignArrayHelperEntries++\n'))
extra=r'''//go:build (linux || darwin) && (amd64 || arm64) && !tinygo
package wago
import("os";"testing";"fmt";"runtime";"unsafe"; gc "github.com/wago-org/wago/src/core/runtime/gc/native")
func TestCampaignArrayHelperEntries(t *testing.T){
 for _,shape:=range []struct{name string;open,promote bool}{{"old-open",true,true},{"nursery-final",false,false},{"old-final",false,true}}{
 c,err:=Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3),gcGeneralScalarArrayGetModule(0x7f,true,shape.open,false));if err!=nil{t.Fatal(err)};defer c.Close()
 path:=fmt.Sprintf("%s-%s.bin",os.Getenv("WAGO_ARRAY_CODE_FILE"),shape.name);file,err:=os.Create(path);if err!=nil{t.Fatal(err)};_,writeErr:=c.WriteCodeTo(file);closeErr:=file.Close();if writeErr!=nil||closeErr!=nil{t.Fatalf("code:%v,%v",writeErr,closeErr)}
 in,err:=Instantiate(c,InstantiateOptions{GC:GCConfig{}});if err!=nil{t.Fatal(err)};defer in.Close()
 if _,err:=in.Invoke("init",17,23);err!=nil{t.Fatal(err)};if shape.promote{if err:=in.CollectGC();err!=nil{t.Fatal(err)};if err:=in.gc.ForcePromote(gc.Ref(uint32(readGlobalObject(in.globalCells[0],in.c.Globals[0].Type))));err!=nil{t.Fatal(err)}}
 view:=in.gc.NativeView();count:=view.HandleCount;ref:=uint32(readGlobalObject(in.globalCells[0],in.c.Globals[0].Type))>>1;handles:=unsafe.Slice((*byte)(offHeapPtr(view.Handles)),int(view.HandleCount)*gc.NativeHandleStride);space:=handles[int(ref)*gc.NativeHandleStride+gc.NativeHandleSpaceOffset];if !shape.promote&&space!=gc.NativeSpaceNursery{t.Fatalf("nursery control space%d",space)};if shape.promote&&space!=gc.NativeSpaceOld{t.Fatalf("old control space%d",space)};t.Logf("shape=%s object_space=%d",shape.name,space)
 for _,mode:=range []string{"fast","fallback","mixed"}{campaignArrayHelperEntries=0
 for j:=0;j<10;j++{view.HandleCount=count;if (mode=="fallback"||mode=="mixed"&&j&1!=0)&&!(runtime.GOARCH=="amd64"&&!shape.open){view.HandleCount=0};got,err:=in.Invoke("loop",100,uint64(j%17));view.HandleCount=count;if err!=nil||len(got)!=1||got[0]!=100{t.Fatalf("loop:%x,%v",got,err)}}
 want:=uint64(1000);native:=os.Getenv("WAGO_ARRAY_EXPECT_GENERAL")=="1"||!shape.promote
 if native{if mode=="fast"{want=0}else if mode=="mixed"{want=500}}
 if runtime.GOARCH=="amd64"&&!shape.open{want=0}
 if campaignArrayHelperEntries!=want{t.Fatalf("%s/%s helpers%d,want%d",shape.name,mode,campaignArrayHelperEntries,want)}
 t.Logf("shape=%s mode=%s guest_ops=1000 helpers=%d code_bytes=%d",shape.name,mode,campaignArrayHelperEntries,c.CodeSize())
 }
 }
}
'''
tp=out/(stage+'-counter-test.go');tp.write_text(extra)
overlay={'Replace':{str(helper):str(hp),str(tree/'src/wago/campaign_array_counter_test.go'):str(tp)}}
(out/(stage+'-counter-overlay.json')).write_text(json.dumps(overlay,indent=2)+'\n')
PY
 expected=0
 if [[ $stage == head && $arch == arm64 ]];then expected=1;fi
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -c -overlay "$out/$stage-counter-overlay.json" -o "$out/$stage-counter.test" ./src/wago > "$out/$stage-counter-build.txt" 2>&1
  cd src/wago
  python3 "$campaign/run.py" build env WAGO_ARRAY_EXPECT_GENERAL="$expected" WAGO_ARRAY_CODE_FILE="$out/$stage-code" "$out/$stage-counter.test" -test.run '^TestCampaignArrayHelperEntries$' -test.count=1 -test.v > "$out/$stage-helper-entries.txt" 2>&1
 )
done
sha256sum "$out/"*-counter-{helper,test}.go "$out/"*-counter.test >> "$out/binary-sha256.txt"
# One serial timing session, with no builds and no instrumented binaries.
python3 "$campaign/run.py" bench python3 - "$out" <<'PY'
import pathlib,subprocess,sys,os,time,json,threading
r=pathlib.Path(sys.argv[1]);os.sched_setaffinity(0,{int(os.environ['WAGO_ARRAY_BENCH_CPU'])})
def external_go():
 rows=subprocess.check_output(['ps','-eo','pid,comm,args'],text=True).splitlines()
 return [x.strip() for x in rows if len(x.split(None,2))==3 and x.split(None,2)[1] in ('go','compile','link')]
for pair in range(1,8):
 attempt=0
 while True:
  attempt+=1
  while external_go():time.sleep(1)
  contaminated=[];stop=threading.Event()
  def monitor():
   with (r/f'pair-{pair}-attempt-{attempt}-processes.jsonl').open('w') as f:
    while not stop.is_set():
     rows=external_go();contaminated.extend(rows);f.write(json.dumps({'time':time.time(),'external_go':rows})+'\n');f.flush();stop.wait(1)
  thread=threading.Thread(target=monitor,daemon=True);thread.start()
  try:
   for stage in (['base','head'] if pair%2 else ['head','base']):
    with (r/f'pair-{pair}-attempt-{attempt}-{stage}.txt').open('w') as f:
     for bench in ['^(BenchmarkGCGeneralScalarArrayGet|BenchmarkInvokeHostFuncDirect)$','^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host[01]$/^n(0|64|1024)$']:
      subprocess.run([str(r/(stage+'.test')),'-test.run=^$','-test.bench='+bench,'-test.benchmem','-test.benchtime=200ms','-test.count=1','-test.cpu=4'],cwd=r/(stage+'-tree')/'src/wago',stdout=f,stderr=subprocess.STDOUT,check=True)
  finally:stop.set();thread.join()
  contaminated.extend(external_go())
  if contaminated:
   (r/f'pair-{pair}-attempt-{attempt}-REJECTED.json').write_text(json.dumps(contaminated,indent=2)+'\n');continue
  for stage in ['base','head']:(r/f'pair-{pair}-{stage}.txt').write_bytes((r/f'pair-{pair}-attempt-{attempt}-{stage}.txt').read_bytes())
  print('accepted pair',pair,'attempt',attempt,flush=True);break
PY
