import collections,json,pathlib
p=pathlib.Path(__file__).parent/'evidence'
lines=['# Native struct sampling diagnostic','', 'Reviewed source `b062646a`; decoder/lifetime unit tests pass. Two 6-second captures after 100 warmups, one locked invocation thread, GOMAXPROCS=1. Kernel/hypervisor CPU excluded; other Go threads excluded. Exact same-build source-map on/off emitted and loaded byte parity passed; checksum and allocation counts passed. No V8 run or speedup claim.','', '| Repeat | Samples | Lost | Native image % | Go symbol % | Other unknown % |','|---|---:|---:|---:|---:|---:|']
records=[]
for i in range(2):
 x=json.loads((p/f'repeat{i}.json').read_text());records.append(x);total=x['total_sample_weight_ns'];native=x['native_resolved_weight_ns'];go=x['go_symbol_weight_total_ns'];unknown=total-native-go
 assert x['lost']==0 and x['journal_status']['dropped_records']==0
 lines.append(f'|{i}|{len(x["samples"])}|{x["lost"]}|{native/total*100:.2f}|{go/total*100:.2f}|{unknown/total*100:.2f}|')
lines+=['','## Native lowering origins','', 'Wasm offsets were checked against function 3 body bytes in the exact artifact: 24 = fb00 struct.new; 35 = fb0e array.set; 42 = fb02 struct.get; 46 = i32.add. Percentages below use ALL sampled thread CPU weight, not just native weight.','', '| Origin | Repeat 0 % | Repeat 1 % |','|---|---:|---:|']
origins=[]
for x in records:
 c=collections.Counter()
 for row in x['resolved']['rows']:
  for pc in row.get('pcs',[]):
   src=pc.get('wasm_source');c[(src['function'],src['wasm_offset']) if src else ('unmapped',None)]+=pc['weight']
 origins.append(c)
for offset,name in [(24,'struct.new'),(35,'array.set'),(42,'struct.get'),(46,'i32.add')]:lines.append('|'+name+'|'+ '|'.join(f'{c[(3,offset)]/x["total_sample_weight_ns"]*100:.2f}' for c,x in zip(origins,records))+'|')
lines+=['', 'All observed native PCs mapped to source ranges, despite only 66.3% static byte coverage. This does not prove unobserved gaps are cold. Constructor plus array publication ranges account for 86.5% and 78.6% of native sampled weight. Scalar get contributes much less, making constructor/publication/barrier inspection the better next diagnostic than another get-forwarding speed guess.', '', 'The hottest repeated native PC is offset 1200 (0x4b0), inside array.set addressing after its bounds check. Disassembly is retained. CPU-clock software samples can skid and reflect adjacent dependency chains; this does NOT establish the shift instruction itself as costly. The source range includes validation, address calculations, remembered-parent logic and fallback code. No sampled call-stack or path-frequency claim is made.', '', '## Go-side observations', '']
for i,x in enumerate(records):
 lines.append(f'Repeat {i}, top flat Go symbol weights:')
 for name,w in sorted(x['go_symbol_weight_ns'].items(),key=lambda z:-z[1])[:6]:lines.append(f'- `{name}`: {w/x["total_sample_weight_ns"]*100:.2f}% of all sampled weight.')
lines+=['', 'Go symbol lookup labels the interrupted PC, not a full/inlined call stack. Native unresolved weight includes these Go PCs; only subtracting the separately symbolized Go share leaves the other-unknown column. Do not add Go weight to native-unresolved as if disjoint.', '', '## Limits and next step', '', 'Weights are software CPU-clock periods in nanoseconds, not hardware cycles, exact elapsed call costs or whole-process CPU. Roughly 600 samples/run give coarse precision. Own local jobs were paused but external host contention is not excluded. No default policy or performance claim follows. Native struct range priorities repeat, while total native/Go split varies. Inspect constructor and array-set emitted sequences and existing proof/barrier reuse opportunities, then test a single correctness-preserving hypothesis with independent throughput evidence. Retained maps are valid only for these captures; future processes need contemporaneous journals.', '', 'Raw samples, image lifetimes, source ranges, counters, binary/artifact/native hashes and decoder test logs are retained. No installation, signal-handler override, cgo or kernel/security-setting change was used.']
(p/'summary.md').write_text('\n'.join(lines)+'\n')
