import fs from 'node:fs';
const out='/tmp/wago-pr564-HbN430';
const all=JSON.parse(fs.readFileSync(`${out}/comparison.json`));
const summary=JSON.parse(fs.readFileSync(`${out}/summary.json`));
const regressions=JSON.parse(fs.readFileSync(`${out}/regressions.json`));
const runs=JSON.parse(fs.readFileSync(`${out}/run-summary.json`));
const inventoryCount=JSON.parse(fs.readFileSync(`${out}/metric-inventory.json`)).entries.length;
const builds=JSON.parse(fs.readFileSync(`${out}/build-size.json`)).rows;
const arm=fs.readFileSync(`${out}/arm64-code-size.tsv`,'utf8').trim().split('\n').slice(1).map(line=>{const[name,base,candidate,delta,percent]=line.split('\t');return{name,base:+base,candidate:+candidate,delta:+delta,percent:+percent};});
const singleWorker=JSON.parse(fs.readFileSync(`${out}/single-worker-summary.json`)).rows;
const pct=x=>x===null||x===undefined?'n/a':`${x>0?'+':''}${x.toFixed(2)}%`;
const time=x=>x>=1e6?`${(x/1e6).toFixed(3)} ms`:x>=1e3?`${(x/1e3).toFixed(3)} us`:`${x.toFixed(2)} ns`;
const find=(mode,name,unit)=>all.find(r=>r.mode===mode&&r.name===name&&r.unit===unit);
const get=(mode,stage,corpus,unit='ns/op')=>summary.find(r=>r.mode===mode&&r.stage===stage&&r.corpus===corpus&&r.unit===unit);
const stages=['Decode','Validate','ValidateWorkers','Compile','CompileCompact','CompileWorkers','CompileFull','CompileFullWorkers','CompileMultiModuleThroughput','Instantiate','Exec','ExecParallel','PluginInstantiate','PluginExec'];
const stageTable=stages.map(stage=>{
  const a=get('explicit',stage,'default'),b=get('signals',stage,'default');
  return `| ${stage} | ${a?.rows??0} | ${pct(a?.delta)} | ${pct(b?.delta)} |`;
}).join('\n');
const largeTable=[];
for(const mode of ['explicit','signals'])for(const mod of ['json-as','lua','sqlite3','ruby','esbuild']) {
  const t=find(mode,`BenchmarkCompileFull/${mod}`,'ns/op');if(!t)continue;
  const heap=find(mode,t.name,'B/op'),alloc=find(mode,t.name,'allocs/op'),code=find(mode,t.name,'code-B');
  largeTable.push(`| ${mode} | ${mod} | ${time(t.base)} | ${time(t.candidate)} | ${pct(t.delta)} | ${pct(heap?.delta)} | ${alloc?.base} -> ${alloc?.candidate} | ${pct(code?.delta)} |`);
}
const confirmed=regressions.filter(r=>r.status==='repeat-significant-slowdown');
const timingTable=confirmed.map(r=>`| ${r.mode} | ${r.name} | ${time(r.base)} | ${time(r.candidate)} | ${pct(r.delta)} | ${r.p.toPrecision(3)} |`).join('\n');
const worstTiming=[...confirmed].sort((a,b)=>b.delta-a.delta).slice(0,6).map(r=>`| ${r.mode} | ${r.name} | ${time(r.base)} | ${time(r.candidate)} | ${pct(r.delta)} |`).join('\n');
const code=all.filter(r=>r.unit==='code-B'&&!/Wazero|_wazero/.test(r.name)&&!r.status);
const codeGrowth=code.filter(r=>r.candidate>r.base);
const changedCode=code.filter(r=>r.candidate!==r.base);
const allocationGrowth=regressions.filter(r=>r.status==='resource-increase-observed');
const peakTable=runs.groups.map(r=>`| ${r.label} | ${r.mode} | ${r.processes} | ${r.failed.length} | ${(r.maxRSSKiB/1024).toFixed(1)} MiB |`).join('\n');
const resources=allocationGrowth.filter(r=>r.unit==='B/op'&&r.delta>1&&/^Benchmark(?:Compile|Validate|Decode|Instantiate|PluginInstantiate)/.test(r.name)).sort((a,b)=>b.delta-a.delta).slice(0,20).map(r=>`| ${r.mode} | ${r.name} | ${r.base} | ${r.candidate} | ${pct(r.delta)} |`).join('\n');
const controlTable=['WazeroCompile','WazeroInstantiate','WazeroExec'].map(stage=>{
  const changes=['explicit','signals'].map(mode=>{
    const rows=all.filter(r=>r.mode===mode&&r.unit==='ns/op'&&r.name.startsWith(`Benchmark${stage}/`)&&!r.name.includes('/isa_'));
    return pct(100*Math.expm1(rows.reduce((sum,r)=>sum+Math.log(r.candidate/r.base),0)/rows.length));
  });
  return `| ${stage} | ${changes.join(' | ')} |`;
}).join('\n');
const buildTable=['manager','runtime-standard','runtime-minimal','runtime-minimal-tiny'].map(name=>{
  const a=builds.find(r=>r.name===name&&r.label==='base'),b=builds.find(r=>r.name===name&&r.label==='candidate');
  return `| ${name} | ${a.bytes.toLocaleString('en-US')} | ${b.bytes.toLocaleString('en-US')} | ${b.bytes-a.bytes>0?'+':''}${b.bytes-a.bytes} | ${pct(100*(b.bytes/a.bytes-1))} | ${b.budget.toLocaleString('en-US')} |`;
}).join('\n');
const armTable=arm.filter(r=>['json-as','lua','sqlite3','ruby','esbuild'].includes(r.name.split('/')[1])).map(r=>`| ${r.name.split('/')[1]} | ${r.base.toLocaleString('en-US')} | ${r.candidate.toLocaleString('en-US')} | ${pct(r.percent)} |`).join('\n');
const singleWorkerTable=singleWorker.map(r=>`| ${r.name} | ${time(r.base)} | ${time(r.candidate)} | ${pct(r.delta)} |`).join('\n');
const report=`# PR #564 correctness and performance qualification

The correctness fixes are local on \`fix/pr564-correctness-performance\`.
No changes were pushed or posted to the PR.

Full-pipeline compile time is lower, but ${confirmed.length} timing slowdowns
remain in the focused repeats. This is **not a zero-regression result**.
The full root suite still has two Wine test failures. Native ARM64 latency is
not measured on this AMD64 host.

- Reviewed PR head: \`ab29bf3a9ad215833b5138220de6cd7190461a78\`.
- Reviewed PR base: \`447f057115ee04d9e58580061dbee696becec21f\`.
- New benchmark baseline, pinned \`origin/main\`: \`a07de0973191efab1d32677eff527952c7f9cdd2\`.
- Qualified production code commit: \`16124d7639983fca0243f77486e32dc5ac74ab57\`.
  Later test/report/data commits do not change production code.

## Changes

- Typed-reference instructions keep exact admission checks. Heap-dependent
  references also keep exact requirement checks. Type-indexed control records
  multi-value, including zero- and one-result block types.
- Tree-based and mixed modules do not publish incomplete validation summaries.
  Summary storage is private, accessors return copies, and the compilation phase
  requires the validated module to stay immutable.
- Fast admission uses an explicit complete instruction-class list. Targeted
  tests disable each feature separately and check that fast acceptance implies
  exact acceptance. They also cover indexed memory and table64 admission.
- Type caches use one import fill walk, followed by local definitions. Optional
  cache storage has a 1 MiB budget and falls back to exact lookups above it.
  Dynamic-call facts reuse the validator's resolved type cache.
- Parallel hint merge checks the total and allocates its destination once.
  Serial and parallel storage share one capacity rule. Workers do not start
  higher-index work after a known error; lower-index work still selects the
  deterministic error.
- AMD64 tests now require the actual 28-byte hint record and the intended
  52-entry medium-function arena chunk. No field was narrowed to reach 24 bytes.
- Address-fact tests compare results and traps with facts on/off across calls,
  local storage, imported globals, and joins at 63/64/65 locals. ARM64 adapter
  tests compare cached/uncached bytes, entries, call targets, and GC metadata.

## Full native AMD64 comparison

Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, \`GOMAXPROCS=8\`, \`GOGC=100\`.
Six fresh process samples per revision, benchmark group, and build/default
bounds mode; 100 ms requested sample time. Baseline/candidate order alternates.
The full default suite includes the generated ISA cases and worker matrices.
Each ratio uses the median of each revision's samples; aggregate ratios are
unweighted geometric means. Negative changes are faster or smaller.

The table excludes ISA cases so they do not dominate the application/compiler
summary. All ISA and per-worker rows remain in the data. Plugin rows are whole
workloads. Row counts are benchmark cases, not independent applications.

| Stage | Non-ISA rows | Explicit build/default | Guard build/default |
|---|---:|---:|---:|
${stageTable}

The mode label describes the build and runtime default. Direct backend
Compile/CompileCompact/CompileWorkers helpers and the standalone JSON benches
explicitly retain their own explicit-bounds setup in both builds. Decode,
validation, and wazero controls are bounds-independent. Public full-pipeline
and normal runtime cases use the selected bounds mode.

Worker matrices contain repeated modules at different worker settings.
MultiModuleThroughput and ExecParallel measure aggregate throughput (elapsed
time divided by total operations), not single-call or single-module latency.
They must not be read as latency gains for one request.

Unchanged-engine controls also move on this shared host:

| Control, non-ISA geomean | Explicit build | Guard build |
|---|---:|---:|
${controlTable}

These control shifts limit claims about small runtime differences. They do not
provide a correction factor for the compiler results, since their samples were
taken at other times. Host-load records and all control samples are retained.

### Five large modules: public compile pipeline

| Mode | Module | Main time | Candidate time | Time change | Heap change | Allocations/op | Code-size change |
|---|---|---:|---:|---:|---:|---:|---:|
${largeTable.join('\n')}

Large-module full-pipeline geomeans: explicit
${pct(get('explicit','CompileFull','five-large')?.delta)}, guard
${pct(get('signals','CompileFull','five-large')?.delta)}.
Large-module backend geomeans: explicit-build
${pct(get('explicit','Compile','five-large')?.delta)}, guard-build
${pct(get('signals','Compile','five-large')?.delta)}.

### Focused implementation checks

These are six-sample, 200 ms component A/B medians within the changed source.
They compare the former lookup strategy with the new one; they are not extra
main-versus-PR whole-compiler samples. Setup is outside the timed operation.

| Component | Former strategy | New strategy | Allocation note |
|---|---:|---:|---|
| Dynamic-call facts, 128 type groups | 59.15 ns | 10.425 ns | 0 B/op, 0 allocs/op in both |
| Dynamic-call facts, 4,096 type groups | 1,853 ns | 10.09 ns | 0 B/op, 0 allocs/op in both |
| Type-cache build, 32 imports | 446.05 ns | 204.55 ns | Raw counts retained |
| Type-cache build, 1,024 imports | 285.725 us | 5.688 us | Raw counts retained |
| Type-cache build, 8,192 imports | 18.403 ms | 43.640 us | 196,608 B/op, 1 alloc/op in both |

These results support linear cache construction and reuse of resolved types.
They do not establish a speedup of that size for a whole module.

## Regressions and limits

The first-pass timing screen repeats every positive change with exact rank-test
\`p < 0.05\`, plus every slowdown above 5%, in 12 alternating-order fresh pairs
with 300 ms requested samples. This is screening across many cases, not a
multiple-comparison-adjusted guarantee. The host also had unrelated active
workloads. Control results and load records are retained.

${confirmed.length} timing rows remain slower with \`p < 0.05\` in the repeat.
The complete list is in
[confirmed timing regressions](bench/results/pr564/confirmed-timing.md).
All observed increases, including small or unconfirmed ones, are in
[regressions.tsv](bench/results/pr564/regressions.tsv). A flat repeat does not
erase the first-pass result; both are stored.

The largest repeated timing increases are shown here. Parallel rows remain
throughput measurements; an 8 ns/op result is not an 8 ns single-call latency.

| Mode | Benchmark | Main | Candidate | Change |
|---|---|---:|---:|---:|
${worstTiming}

For swar-pack-parse, fib_iter, xjb-mulhi, and the corpus globals module, the
native code hashes, exact entry values, and prepared-call routing flags match
between revisions in eight module/configuration checks. This is narrower than
whole-runtime equivalence and does not attribute the observed timing change.
The corpus globals check is not the standalone GlobalGet microbenchmark.
The 4.17% i64x2 shift row is borderline (p about 0.04995) in this unadjusted
multi-case screen; retain it as a watch item, not a proven source-level cause.

A separate 12-pair, 300 ms check with GOMAXPROCS=1 does not reproduce the three
parallel-call regressions (benchstat p=0.417, 0.514, and 0.503). GlobalGet remains
slower (p=0.004). These samples are not mixed into the eight-worker comparison.

| Single-worker diagnostic | Main | Candidate | Change |
|---|---:|---:|---:|
${singleWorkerTable}

The parallel costs are sensitive to worker count in this harness. Their exact
cause remains unresolved; matching native bytes and a flat one-worker result
do not erase the eight-worker regression. Investigate per-instance data layout
and host-call behavior under concurrent execution before claiming unchanged
runtime performance. This is a follow-up direction, not a proven attribution.

There are ${allocationGrowth.length} observed resource-increase rows. These
include heap bytes, allocation counts, and normalized per-call counters; they
are not all statistically established regressions. The following are up to 20
compiler/setup heap increases above 1%; the full list has no percentage cutoff.

| Mode | Benchmark | Main B/op | Candidate B/op | Change |
|---|---|---:|---:|---:|
${resources}

ExecParallel also reports small allocation-counter increases, such as 2 -> 7
B/op for independent matmul. Its timed RunParallel setup allocates worker and
closure state once, then amortizes that cost over the calibrated iteration
count. These values are not isolated steady-state call allocations. All such
raw counter changes remain in the data, with this interpretation limit.

Separate one-iteration instrumented checks locate part of this small-module
cost in the changed arena policy: reserved operand-node storage for isa_call is
6,776 -> 8,400 bytes; for isa_var it is 13,888 -> 14,336 bytes. isa_var's hint
sidecar is 32 -> 116 bytes under the common serial/parallel capacity contract.
These counters explain part, not all, of Go's heap increase. Their instrumented
times and heap totals are excluded from the normal comparison.

Across ${code.length} paired native-code-size rows, ${changedCode.length} sizes
changed and ${codeGrowth.length} grew. Size equality does not prove byte equality
or semantic equivalence. ARM64 changed native output in the PR and must not be
described as compile-time-only. Native ARM64 speed was not measured on this host;
QEMU checks are correctness/code-size evidence only.

The new ARM64 explicit-bounds census has ${arm.length} paired modules, including
ISA fixtures: ${arm.filter(r=>r.delta<0).length} shrink,
${arm.filter(r=>r.delta===0).length} are equal, and ${arm.filter(r=>r.delta>0).length} grow.

| ARM64 module | Main code bytes | Candidate code bytes | Change |
|---|---:|---:|---:|
${armTable}

### Built binary footprint

Both revisions use the CI profile flags with Go 1.22.12, TinyGo 0.41.1,
LLVM 20.1.1, CGO disabled, and version 0.0.0. VCS stamping is disabled for the
detached baseline worktree. Tool versions, exact commands, and failed initial
VCS-stamping attempt are retained. Build times are not part of the JIT timing
comparison. Every candidate profile is below its checked-in byte budget.

| Profile | Main bytes | Candidate bytes | Delta bytes | Change | Budget bytes |
|---|---:|---:|---:|---:|---:|
${buildTable}

Execution allocation units need care: the batched harness normalizes time per
call but leaves Go's B/op and allocs/op counters per batch. The analysis adds
derived B/call and allocs/call from each sample before taking medians.
PluginExec stops Go's allocation timer; its printed zeros do **not** establish
allocation-free plugin execution.

### Process memory and run coverage

| Revision | Build/default mode | Processes | Failed | Largest RSS |
|---|---|---:|---:|---:|
${peakTable}

RSS includes fixtures, Go heap, native mappings, and test infrastructure for the
whole group. Faster code can also run more iterations in the requested time.
It is not an isolated hint-scan peak measurement or a per-operation footprint.
Worker buffers still overlap the final hint destination; one destination
allocation removes intermediate merge growth but not that overlap.

The 139 focused repeat groups add 3,336 successful processes. The separate
one-worker diagnosis adds 96 successful processes. The repeat runner was drained
and paused once for correctness, size, and tool checks; none overlapped a timed
sample. One wrapper-duration field includes that pause; its benchmark and GNU
time measurements finished before the pause work. See \`confirmation-pause.json\`.

The initial unsplit baseline process received SIGKILL during CompileCompact;
the cause was not established. Its subsequent guard run was stopped. These
failed/incomplete captures are retained and excluded from the paired comparison.
The fresh-process comparison uses identical boundaries for both revisions.

The optional external Impart \`sqli.wasm\` fixture was unavailable, so its row
is visibly skipped. The candidate-only optimization-ablation matrix is opt-in
and was not enabled; it has no baseline counterpart. Neither is silently counted
as a measured case. The guard-only memory benchmark is included separately.

## Correctness qualification

- Focused validator, frontend, shared, AMD64, and runtime regression tests pass.
- Native compiler race tests pass.
- Native runtime tests pass with the pinned reference interpreter.
- Guard-page runtime tests and corpus differential tests pass.
- Benchmark-module tests pass.
- Core 2 spec run: 1,600 modules, 48,331 assertions, zero failures or gaps.
- Selected ARM64 backend and runtime tests pass under QEMU, including explicit
  and guard-page address-fact cases. This does not replace native ARM64 testing.

The full root \`go test ./...\` run was **not green**. In its final Go 1.22.12 run,
all packages other than the root package pass. Its two Wine test failures are installer
checksum verification and an unsupported directory operation during install.
The local Wine 10.0 certutil check exits zero without printing a hash for a known
file. Supplying official Windows curl in a temporary directory fixes download
availability but does not fix checksum verification. The curl source and digest
are retained in \`wine-tools.json\` ([publisher](https://curl.se/windows/)).
No check was weakened.

The earlier installed TinyGo 0.42.0 / Go 1.27.1 duplicate-symbol failure is
resolved with the CI-pinned TinyGo 0.41.1 / Go 1.22.12 pair; the full standalone
package passes. Pinned WABT/reference-interpreter and scoped CLI settings resolve
the other setup failures. The stronger mixed-local test passes on AMD64 and
emulated ARM64 in both bounds modes. An ARM64 corpus test launched from the
wrong directory failed; its rerun from the correct package directory passes.
The retained failed captures and successful reruns are included, with a
[test-command summary](bench/results/pr564/test-summary.md).

## Evidence retained for later comparisons

[Data and commands](bench/results/pr564/README.md) include raw captures,
all sample medians, exact rank-test screens, benchstat output, focused repeats,
per-process exit status/RSS, host/tool details, test logs, and SHA-256 checksums.

[Reported-metric inventory](bench/results/pr564/inventory-summary.md) indexes
${inventoryCount.toLocaleString('en-US')} numeric/performance-claim lines from the PR description, report, commit
messages, and attached CI size artifact. It keeps all historical sources and checkpoints separate from this
qualification. The historical 24-byte claim and unchanged-ARM64-code claim do
not describe the reviewed implementation.
`;
fs.writeFileSync(`${out}/REPORT-new.md`,report);
fs.writeFileSync(`${out}/confirmed-timing.md`,`# Timing slowdowns after focused repeats\n\nThese are 12-pair, 300 ms requested-time results. See the main report for host\nnoise, multiple-comparison, and benchmark-unit limits. All first-pass data\nremain in the comparison files.\n\n| Mode | Benchmark | Main | Candidate | Change | Exact rank p |\n|---|---|---:|---:|---:|---:|\n${timingTable}\n`);
console.log(`Rendered report and ${confirmed.length} confirmed timing rows.`);
