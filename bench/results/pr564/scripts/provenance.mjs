import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
const out='/tmp/wago-pr564-HbN430';
const run=(...args)=>execFileSync(args[0],args.slice(1),{encoding:'utf8'}).trim();
const data={
  pr:564,
  reviewedBase:'447f057115ee04d9e58580061dbee696becec21f',
  reviewedHead:'ab29bf3a9ad215833b5138220de6cd7190461a78',
  baseline:'a07de0973191efab1d32677eff527952c7f9cdd2',
  candidate:'16124d7639983fca0243f77486e32dc5ac74ab57',
  strengthenedTestsCommit:run('git','rev-parse','b0da7d478'),
  branch:run('git','branch','--show-current'),
  go:run('go','version'),
  platform:run('uname','-a'),
  dateUTC:new Date().toISOString(),
  env:{GOMAXPROCS:'8',GOGC:'100',GOMEMLIMIT:'not set'},
  method:{samples:6,benchtime:'100ms',bounds:['explicit','signals'],ISA:true,processBoundary:'one top-level benchmark and sample',order:'baseline/candidate on even samples; candidate/baseline on odd samples',timeLimit:'10m per process',peakMemory:'GNU time 1.9 maximum resident set size in KiB'},
  buildCommands:['cd bench && go test -c -o <label>-explicit.test .','cd bench && go test -tags wago_guardpage -c -o <label>-signals.test .'],
  sourceNotes:['Baseline binaries built at detached origin/main before edits.', 'Candidate includes a merge of that origin/main into reviewed PR, followed by correctness and bounded-performance fixes.', 'No benchmark/test/build work from this task overlaps paired timing runs. Other user processes on this workstation remain running.', 'Historical Apple ARM64 and Rosetta AMD64 results are not numerically compared with this Linux AMD64 host.', 'Opt-in optimization ablation matrix is excluded from the default full suite; its skip is retained.'],
  failures:['Initial single-process baseline explicit run was SIGKILL during CompileCompact/xjb-mulhi; no Go error was printed. The cause was not established. Initial signals run was then stopped with SIGTERM. Both captures are retained and excluded from paired statistics.'],
  interpretationNotes:['The mode label records build tags and WAGO_BOUNDS. Public CompileFull, instance, execution, and plugin paths use that runtime default. Direct backend Compile/CompileCompact/CompileWorkers helpers pass their own default CompileOptions and remain explicit-bounds in both builds. Standalone JsonAsSerialize/Deserialize_wago explicitly select explicit bounds in both builds. Decode/Validate and wazero controls are bounds-independent.', 'BenchmarkPluginExec stops the Go benchmark timer and reports one manually timed whole workload. Its printed iteration count is calibration scaffolding, not workload repetitions; its printed zero B/op and allocs/op do not establish allocation-free plugin execution. Six fresh process samples still provide six workload observations per revision.', 'Peak RSS includes fixture data, Go heap, native mappings, test infrastructure, and all cases in the process. It is not per-operation allocation or isolated hint-scan peak memory.'],
  binaryHashes:Object.fromEntries(['base-explicit','base-signals','candidate-explicit','candidate-signals','base-arm64-bench','candidate-arm64-bench'].map(n=>[n,run('sha256sum',`${out}/${n}.test`).split(' ')[0]])),
  binaryBuildInfo:Object.fromEntries(['base-explicit','base-signals','candidate-explicit','candidate-signals'].map(n=>[n,run('go','version','-m',`${out}/${n}.test`)])),
  benchstatBuildInfo:run('go','version','-m',`${out}/bin/benchstat`),
  allocationUnits:'The batched Exec and WazeroExec harness reports ns/op per call, but its Go B/op and allocs/op counters remain per batch. The analysis retains raw units and adds B/call and allocs/call by dividing each sample by its calls/batch before taking medians. Derived values inherit printed-counter rounding.',
  toolVersions:{wast2json:run('.tools/wabt-1.0.41-linux-x64/bin/wast2json','--version'),specInterpreterRevision:'9d36019973201a19f9c9ebb0f10828b2fe2374aa',qemu:run(`${out}/qemu/usr/bin/qemu-aarch64`,'--version').split('\n')[0]},
  confirmationPause:JSON.parse(fs.readFileSync(`${out}/confirmation-pause.json`)),
  buildSizeTools:JSON.parse(fs.readFileSync(`${out}/build-size.json`)),
  wineTool:JSON.parse(fs.readFileSync(`${out}/wine-tools.json`)),
  parallelAllocationLimit:'ExecParallel B/op and allocs/op include one-time RunParallel worker/closure setup amortized over the calibrated iteration count. These are not isolated steady-state invocation-allocation measurements.',
  singleWorkerDiagnostic:JSON.parse(fs.readFileSync(`${out}/single-worker-summary.json`)),
};
fs.writeFileSync(`${out}/provenance.json`,JSON.stringify(data,null,2)+'\n');
console.log(JSON.stringify(data.binaryHashes,null,2));
