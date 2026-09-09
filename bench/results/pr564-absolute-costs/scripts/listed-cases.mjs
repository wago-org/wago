import fs from 'node:fs';
import {gunzipSync} from 'node:zlib';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs';
const rows=JSON.parse(gunzipSync(fs.readFileSync('/home/jtenner/Projects/wago/bench/results/pr564-regression-fixes/full/comparison.json.gz')));
const listed=[
 ['BenchmarkExec/coremark.coremark_run',34336849.5],
 ['BenchmarkExecParallel/independent/isa_simd_i64x2.extend_high_s',300.6],
 ['BenchmarkExecParallel/independent/isa_ctl.if_else',1771.5],
 ['BenchmarkExecParallel/independent/isa_mem.store_i32_stride',1823],
 ['BenchmarkExecParallel/independent/float.run',620.05],
 ['BenchmarkExecParallel/independent/json-as.deserializeN',6794.5],
 ['BenchmarkExecParallel/independent/isa_mem.store_i32_stride',1597.5],
 ['BenchmarkExecParallel/independent/isa_f64.sqrt',22819.5],
 ['BenchmarkCompileCompact/fib_rec',17187],
 ['BenchmarkExecParallel/independent/isa_i64.rotl',2229],
 ['BenchmarkExecParallel/independent/isa_simd_i8x16.shr_s',660.55],
 ['BenchmarkExecParallel/process/float.run',647.7],
 ['BenchmarkExecParallel/independent/isa_simd_i64x2.shl',171.65],
 ['BenchmarkPluginExec/wasm3',22318839.5],
 ['BenchmarkExecParallel/independent/spectralnorm.run',100423.5],
 ['BenchmarkExecParallel/independent/matmul.run',16769.5],
 ['BenchmarkExecParallel/independent/nbody.step',40507.5],
 ['BenchmarkExecParallel/process/isa_simd_f32x4.abs',263.85],
 ['BenchmarkExecParallel/independent/arith.run',211.85],
 ['BenchmarkExecParallel/independent/isa_ctl.br_table',3238],
 ['BenchmarkExecParallel/independent/isa_ctl.br_table',2984],
 ['BenchmarkExecParallel/process/isa_bulk_mem.copy_fwd_64',351.8],
 ['BenchmarkPluginInstantiate/ruby',2195357],
];
const mapped=listed.map(([name,base])=>{
 const hits=rows.filter(r=>r.name===name&&r.unit==='ns/op'&&Math.abs(r.base-base)<1e-7);
 if(hits.length!==1)throw Error(`Expected one old row for ${name}/${base}, got ${hits.length}`);
 const r=hits[0];return {mode:r.mode,name,previousMain:r.base,previousAfter:r.candidate,previousDelta:r.delta,previousP:r.p};
});
fs.writeFileSync(`${root}/listed-cases.json`,JSON.stringify(mapped,null,2));
