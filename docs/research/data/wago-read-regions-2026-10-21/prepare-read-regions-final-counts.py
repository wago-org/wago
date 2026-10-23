from pathlib import Path
import hashlib,io,json,os,shutil,subprocess,tarfile,time
root=Path('/home/dev/Projects/dewdrop');out=root/'.tmp/wago-read-regions-final-counts';out.mkdir(exist_ok=False)
installed=root/'.tmp/wago-local-stack-2026-10-20/wago';candidate=root/'.tmp/wago-gc-read-regions';oldhost=root/'.tmp/wago-batch-cancel-full/host-source'
base='073b2f61880e965d3c33ba83e32f72e9391a845d';assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=installed,text=True).strip()==base
files=['src/core/compiler/backend/railshot/amd64/'+f for f in ['call.go','compile.go','driver.go','gc_struct.go','inline.go','gc_cast_array_get.go','gc_read_region.go']]
for name in files[:-1]:assert subprocess.check_output(['git','show',base+':'+name],cwd=installed)==subprocess.check_output(['git','show','HEAD:'+name],cwd=candidate)

def patch_region_counter(source):
 p=source/'src/core/compiler/backend/railshot/amd64'
 helper=r'''//go:build amd64
package amd64
import("encoding/json";"os";"unsafe")
// Diagnostic only: static storage lives for the whole process. These absolute
// addresses must never be installed or reused from a cache in another process.
var readRegionCounters [65536][7]uint64
func (f *fn) countReadRegion(kind int) {
 if f.traceFuncIdx>=65536 { panic("diagnostic read-region target exceeds counter bound") }
 ptr:=uintptr(unsafe.Pointer(&readRegionCounters[f.traceFuncIdx][kind]))
 f.a.EmitBytes([]byte{0x9c}) // pushfq: preserve deferred comparison flags
 f.a.Push(RAX)
 f.a.MovImm64(RAX,uint64(ptr))
 f.a.Push(RDX)
 f.a.Load64(RDX,RAX,0)
 f.a.AluRI(0,RDX,1,true)
 f.a.Store64(RAX,0,RDX)
 f.a.Pop(RDX)
 f.a.Pop(RAX)
 f.a.EmitBytes([]byte{0x9d}) // popfq
}
func SaveReadRegionCounts(path string) {
 rows:=make(map[int][7]uint64)
 for i,row:=range readRegionCounters { if row[0]!=0 { rows[i]=row } }
 b,e:=json.MarshalIndent(map[string]any{"kinds":[]string{"entries","numeric","checked_struct_attempts","reused_struct","array_attempts","stack_caches","stack_reuses"},"targets":rows},"","  ")
 if e!=nil{panic(e)};if e=os.WriteFile(path,b,0600);e!=nil{panic(e)}
}
'''
 (p/'read_region_diagnostic.go').write_text(helper)
 f=p/'inline.go';text=f.read_text();a=text.index('func (f *fn) inlineBodyCtrl(');pos=text.index('\n\tf.flush()',a)+len('\n\tf.flush()');text=text[:pos]+'\n if t.readRegion!=nil { f.countReadRegion(0) }'+text[pos:];f.write_text(text)
 f=p/'gc_read_region.go';text=f.read_text().replace('f.stats.peep("gc-read-region-array")','f.countReadRegion(4)\n f.stats.peep("gc-read-region-array")').replace('f.stats.peep("gc-read-region-reuse")','f.countReadRegion(3)\n f.stats.peep("gc-read-region-reuse")').replace('f.stats.peep("gc-read-region-resolve")','f.countReadRegion(2)\n f.stats.peep("gc-read-region-resolve")').replace('f.stats.peep("gc-read-region-stack-cache")','f.countReadRegion(5)\n f.stats.peep("gc-read-region-stack-cache")').replace('f.stats.peep("gc-read-region-stack-reuse")','f.countReadRegion(6)\n f.stats.peep("gc-read-region-stack-reuse")').replace('f.stats.peep("gc-read-region-numeric")','f.countReadRegion(1)\n f.stats.peep("gc-read-region-numeric")');f.write_text(text)
archive=subprocess.check_output(['git','archive',base],cwd=installed);rows=[]
for variant in ['candidate']:
 directory=out/variant;directory.mkdir();source=directory/'source';source.mkdir();host=directory/'host-source';host.mkdir();start=time.monotonic()
 with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(source,filter='data')
 delta={}
 if variant=='candidate':
  for name in files:
   content=(candidate/name).read_bytes();(source/name).write_bytes(content);delta[name]=hashlib.sha256(content).hexdigest()
  patch_region_counter(source)
 subprocess.run(['git','init','-q',str(source)],check=True)
 patch=root/'tools/wago-transform-host/patches/managed-isolated-fork.patch';subprocess.run(['git','-C',str(source),'apply','--check',str(patch)],check=True);subprocess.run(['git','-C',str(source),'apply',str(patch)],check=True)
 for path in [oldhost/'go.mod',oldhost/'go.sum',*oldhost.glob('*.go')]:shutil.copyfile(path,host/path.name)
 f=source/'cli/runtime/commands/run/command.go';contents=f.read_text()
 contents=contents.replace('import (','import (\n readregiondiag "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"',1)
 assert contents.count('finish := func() error {')==1
 contents=contents.replace('finish := func() error {','finish := func() error {\n defer readregiondiag.SaveReadRegionCounts(os.Getenv("WAGO_REGION_COUNTS_OUT"))',1)
 f.write_text(contents)
 mod=host/'go.mod';oldsource=str(root/'.tmp/wago-batch-cancel-full/source');assert mod.read_text().count(oldsource)==1;mod.write_text(mod.read_text().replace(oldsource,str(source)))
 env=dict(os.environ,GOWORK='off',CGO_ENABLED='0',GOMAXPROCS='2',GOFLAGS='-buildvcs=false');binary=root/'.tmp/wago-local-perf-2026-10-20'/('read-regions-final-counts-'+variant+'-host')
 cmd=['go','build','-mod=mod','-ldflags=-X main.runtimePatchIdentity=read-regions-'+variant+' -X main.workerPluginIdentity=read-regions-'+variant,'-o',str(binary),'.'];t=time.monotonic();p=subprocess.run(cmd,cwd=host,env=env,capture_output=True,text=True)
 row=dict(variant=variant,source_commit=base,candidate_production_delta=delta,command=cmd,seconds=time.monotonic()-t,total_prepare_seconds=time.monotonic()-start,exit=p.returncode,stdout=p.stdout,stderr=p.stderr,managed_patch_sha256=hashlib.sha256(patch.read_bytes()).hexdigest());rows.append(row)
 if not p.returncode:row['binary_sha256']=hashlib.sha256(binary.read_bytes()).hexdigest()
 (out/'build.json').write_text(json.dumps(rows,indent=2));print(json.dumps(row),flush=True);assert not p.returncode
