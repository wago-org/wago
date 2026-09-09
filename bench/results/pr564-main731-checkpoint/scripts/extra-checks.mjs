// Run only after all timed comparisons have finished.
import fs from 'node:fs';
import {spawnSync,execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
const repo='/home/jtenner/Projects/wago',root=`${repo}/.tmp/pr564-memory/final731`;
const goDir='/home/jtenner/.local/share/mise/installs/go/1.22.12/bin';
const tinyRoot=`${repo}/.tmp/pr564-absolute-costs/pinned-tinygo`;
const env={...process.env,PATH:`${goDir}:${process.env.PATH}`,GOMAXPROCS:'8',GOTOOLCHAIN:'local',GOFLAGS:'-buildvcs=false',CGO_ENABLED:'0',GOOS:'linux',GOARCH:'amd64',TINYGOROOT:tinyRoot,TINYGOCACHE:`${root}/pinned-tinygo-cache`};
delete env.GOROOT;delete env.WAGO_HOME;
const rows=[];
function run(name,command,args,cwd=repo) {
 const start=Date.now(),r=spawnSync(command,args,{cwd,env,encoding:'utf8',timeout:180000});
 fs.writeFileSync(`${root}/${name}.txt`,(r.stdout??'')+(r.stderr??''));
 const row={name,command,args,cwd,status:r.status,signal:r.signal,error:r.error?.message,seconds:(Date.now()-start)/1000};
 rows.push(row);fs.writeFileSync(`${root}/extra-checks.json`,JSON.stringify(rows,null,2)+'\n');console.log(row);return r;
}
const binary=`${root}/size/final-runtime-standard-tiny`;
if(execFileSync('git',['rev-parse','HEAD'],{cwd:repo,encoding:'utf8'}).trim()!=='1d04b458fa512265e2b93711f5c355435a29f33e')throw Error('Production identity changed');
const build=run('standard-tiny-build',`${tinyRoot}/bin/tinygo`,['build','-scheduler=tasks','-no-debug','-opt=z','-gc=conservative','-ldflags','-X main.version=0.0.0','-tags','wago_runtime,wago_lean','-o',binary,'./cli/wago']);
let smokeFailed=true;
if(build.status===0) {
 const strip=run('standard-tiny-strip','strip',['-s','--strip-section-headers','--remove-section=.eh_frame','--remove-section=.eh_frame_hdr','--remove-section=.comment',binary]);
 if(strip.status===0) {
  const samples=[];
  const cases=[['version',['--version'],s=>s.includes('tinygo 0.41.1')],['fib20',['run','--invoke','fib','tests/fixtures/wasm/fib.wasm','20'],s=>s.trim()==='fib(20) = 6765'],['fib30',['run','--invoke','fib','tests/fixtures/wasm/fib.wasm','30'],s=>s.trim()==='fib(30) = 832040'],['hypot',['run','--invoke','hypot','tests/fixtures/wasm/fprog.wasm','3.0','4.0'],s=>s.includes('= 5')]];
  const identity={binary,bytes:fs.statSync(binary).size,sha256:createHash('sha256').update(fs.readFileSync(binary)).digest('hex'),head:'1d04b458fa512265e2b93711f5c355435a29f33e',rows:samples};
  for(let repeat=0;repeat<20;repeat++)for(const[name,args,check]of cases) {
   const r=spawnSync(binary,args,{cwd:repo,env:{...env,WAGO_HOME:`${root}/extra-cli-home`},encoding:'utf8',timeout:15000});
   const sample={name,args,repeat,status:r.status,signal:r.signal,error:r.error?.message,stdout:r.stdout,stderr:r.stderr,passed:r.status===0&&check(r.stdout??'')};samples.push(sample);
   fs.writeFileSync(`${root}/standard-tiny-smoke.json`,JSON.stringify(identity,null,2)+'\n');if(!sample.passed)console.log(sample);
  }
  smokeFailed=samples.some(r=>!r.passed);console.log({standardTinySamples:samples.length,failed:samples.filter(r=>!r.passed).length,bytes:identity.bytes});
 }
}
for(const[label,cwd]of [['main',`${repo}/.tmp/pr564-memory/main-latest`],['final',repo]])run(`${label}-wine-installer`,`${goDir}/go`,['test','.','-run','^TestWineInstallerCompletesNativeInstallFlow$','-count=1','-timeout=2m'],cwd);
// Keep any local test failure visible in the exit status and saved transcript.
if(smokeFailed||rows.some(r=>r.status!==0))process.exitCode=1;
