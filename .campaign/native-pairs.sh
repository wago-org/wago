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
case "$(uname -m):$arch" in x86_64:amd64|aarch64:arm64) ;; *) echo 'Native AMD64 or ARM64 required' >&2; exit 2;; esac
cpu=$(python3 -c 'import os; a=sorted(os.sched_getaffinity(0));print(a[min(2,len(a)-1)])')
export WAGO_STRUCT_BENCH_CPU="$cpu"
{
 date -u
 uname -a
 go version
 go env GOOS GOARCH
 lscpu
 python3 -c 'import os;print("available_affinity="+str(sorted(os.sched_getaffinity(0))));print("benchmark_cpu="+os.environ["WAGO_STRUCT_BENCH_CPU"])'
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
harness=src/wago/gc_struct_get_packed_native_test.go
git -C "$repo" show "$head:$harness" > "$out/base-tree/$harness"
sha256sum "$out/base-tree/$harness" "$out/head-tree/$harness" > "$out/overlay-sha256.txt"
git -C "$out/base-tree" add -N -- "$harness"
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
 python3 "$campaign/run.py" build go test -p 4 -tags=wago_regalloccheck,wago_codegenstats ./src/core/compiler/backend/railshot/"$arch" -run 'TestGCPackedStructGet|TestGCGeneralArrayLen|TestGCObjectStub|TestArrayLenParameter|TestModuleGCResolverFamilyCounts|TestGCResolvedHandleReuse|TestPatchCallRelocs|TestCallRelocTable' -count=1 -v > "$out/backend-correctness.txt" 2>&1
)
sha256sum "$out/base.test" "$out/head.test" "$out/head-debug.test" > "$out/binary-sha256.txt"
for stage in base head head-debug; do
 tree=$stage
 if [[ $stage == head-debug ]];then tree=head;fi
 (cd "$out/$tree-tree/src/wago"; python3 "$campaign/run.py" build "$out/$stage.test" -test.run '^TestGCPackedStructGet' -test.count=1 -test.v > "$out/$stage-correctness.txt" 2>&1)
done
# Counter builds use overlays. The normal timing binaries above stay unchanged.
python3 - "$out" <<'PY'
import pathlib,sys,json
r=pathlib.Path(sys.argv[1])
extra=r'''
func TestCampaignStructHelperEntries(t *testing.T) {
 c,err:=Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3),gcPackedStructGetModule(0x78,true,true,true,false,0));if err!=nil{t.Fatal(err)};defer c.Close()
 file,err:=os.Create(os.Getenv("WAGO_STRUCT_CODE_FILE"));if err!=nil{t.Fatal(err)}
 _,writeErr:=c.WriteCodeTo(file);closeErr:=file.Close();if writeErr!=nil||closeErr!=nil{t.Fatalf("code dump:%v,%v",writeErr,closeErr)}
 in,err:=Instantiate(c,InstantiateOptions{});if err!=nil{t.Fatal(err)};defer in.Close()
 if _,err:=in.Invoke("init",17);err!=nil{t.Fatal(err)}
 view:=in.gc.NativeView();count:=view.HandleCount
 for _,mode:=range []string{"fast","fallback","mixed"}{
  campaignStructHelperEntries=0
  for j:=0;j<10;j++{
   view.HandleCount=count
   if mode=="fallback"||mode=="mixed"&&j&1!=0{view.HandleCount=0}
   got,err:=in.Invoke("loop",100);view.HandleCount=count
   if err!=nil||len(got)!=1||got[0]!=100{t.Fatalf("loop:%v,%v",got,err)}
  }
  want:=uint64(1000)
  if os.Getenv("WAGO_STRUCT_EXPECT_NATIVE")=="1"{if mode=="fast"{want=0}else if mode=="mixed"{want=500}}
  if campaignStructHelperEntries!=want{t.Fatalf("mode%s:helpers%d,want%d",mode,campaignStructHelperEntries,want)}
  t.Logf("mode=%s guest_ops=1000 helpers=%d code_bytes=%d",mode,campaignStructHelperEntries,c.CodeSize())
 }
}
'''
for stage in ['base','head']:
 tree=r/(stage+'-tree');helper=tree/'src/wago/gc_struct_helper.go';test=tree/'src/wago/gc_struct_get_packed_native_test.go'
 original=helper.read_text();assert original.count('case gcStructGet, gcStructGetS, gcStructGetU:\n')==1
 instrumented=original.replace('const (','var campaignStructHelperEntries uint64\n\nconst (',1).replace('case gcStructGet, gcStructGetS, gcStructGetU:\n','case gcStructGet, gcStructGetS, gcStructGetU:\n campaignStructHelperEntries++\n',1)
 hp=r/(stage+'-counter-helper.go');hp.write_text(instrumented)
 tp=r/(stage+'-counter-test.go');tp.write_text(test.read_text().replace('"fmt"','"fmt"\n "os"',1)+'\n'+extra)
 (r/(stage+'-counter-overlay.json')).write_text(json.dumps({'Replace':{str(helper):str(hp),str(test):str(tp)}},indent=2)+'\n')
PY
for stage in base head;do
 expected=0
 if [[ $stage == head && $arch == arm64 ]];then expected=1;fi
 (
  cd "$out/$stage-tree"
  python3 "$campaign/run.py" build go test -p 4 -c -overlay "$out/$stage-counter-overlay.json" -o "$out/$stage-counter.test" ./src/wago > "$out/$stage-counter-build.txt" 2>&1
  cd src/wago
  python3 "$campaign/run.py" build env WAGO_STRUCT_EXPECT_NATIVE="$expected" WAGO_STRUCT_CODE_FILE="$out/$stage-code.bin" "$out/$stage-counter.test" -test.run '^TestCampaignStructHelperEntries$' -test.count=1 -test.v > "$out/$stage-helper-entries.txt" 2>&1
 )
done
sha256sum "$out/"*-counter-{helper,test}.go "$out/"*-counter.test >> "$out/binary-sha256.txt"
# One serial timing session, with no builds and no instrumented binaries.
python3 "$campaign/run.py" bench python3 - "$out" <<'PY'
import pathlib,subprocess,sys,os,time,json,threading
r=pathlib.Path(sys.argv[1]);os.sched_setaffinity(0,{int(os.environ['WAGO_STRUCT_BENCH_CPU'])})
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
     for bench in ['^(BenchmarkGCPackedStructGet|BenchmarkInvokeHostFuncDirect)$','^BenchmarkHostRoundtripLoop$/^mem1$/^parallel(false|true)$/^host[01]$/^n(0|64|1024)$']:
      subprocess.run([str(r/(stage+'.test')),'-test.run=^$','-test.bench='+bench,'-test.benchmem','-test.benchtime=200ms','-test.count=1','-test.cpu=4'],cwd=r/(stage+'-tree')/'src/wago',stdout=f,stderr=subprocess.STDOUT,check=True)
  finally:stop.set();thread.join()
  contaminated.extend(external_go())
  if contaminated:
   (r/f'pair-{pair}-attempt-{attempt}-REJECTED.json').write_text(json.dumps(contaminated,indent=2)+'\n');continue
  for stage in ['base','head']:(r/f'pair-{pair}-{stage}.txt').write_bytes((r/f'pair-{pair}-attempt-{attempt}-{stage}.txt').read_bytes())
  print('accepted pair',pair,'attempt',attempt,flush=True);break
PY
