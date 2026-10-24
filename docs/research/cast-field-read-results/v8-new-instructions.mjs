import fs from 'node:fs';
import {performance} from 'node:perf_hooks';
const [record, dynamic] = process.argv.slice(2);
if (!record || !dynamic) throw new Error('Pass the record and dynamic-array Wasm fixture paths');
const rows=[];
for (const [file,names] of [[record,['loop-i32','loop-v128']],[dynamic,['alternating-fill-dynamic']]]) {
 const bytes=fs.readFileSync(file); let t=performance.now(); const m=new WebAssembly.Module(bytes); const compileMs=performance.now()-t;
 const e=new WebAssembly.Instance(m).exports; if(e.init) e.init();
 for(const name of names) {
  const n=8192, want=name==='loop-i32'?7*n:name==='loop-v128'?22*n:n*(n-1);
  const run=()=>{const got=e[name](n);if((Number(got)>>>0)!==(want>>>0))throw new Error(`${name}: ${got}`);};
  for(let i=0;i<1000;i++) run();
  const ms=[]; for(let j=0;j<5;j++){t=performance.now(); for(let i=0;i<1000;i++)run();ms.push((performance.now()-t)/1000);}
  rows.push({file,name,wasm_compile_ms:compileMs,iterations:1000,guest_iterations_per_invoke:n,milliseconds_per_invoke:ms});
 }
}
console.log(JSON.stringify({node:process.version,v8:process.versions.v8,rows},null,2));
