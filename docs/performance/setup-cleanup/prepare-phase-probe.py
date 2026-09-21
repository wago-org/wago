#!/usr/bin/env python3
"""Generate instrumentation only in .tmp; never edit production source."""
from pathlib import Path
import json
ROOT=Path(__file__).resolve().parents[3]
TMP=ROOT/'.tmp/setup-cleanup'
source=(ROOT/'src/wago/instantiate.go').read_text()
source=source.replace('import (','import (\n probetime "time"\n probeatomic "sync/atomic"',1)
def insert(marker, before='', after=''):
 global source
 if source.count(marker)!=1: raise SystemExit(f'expected one marker: {marker!r}; got {source.count(marker)}')
 source=source.replace(marker,before+marker+after,1)
def span(index,start,end):
 insert(start,before=f'probeStart{index} := probetime.Now()\n')
 insert(end,before=f'lifecycleProbeEnd({index}, probeStart{index})\n')
insert('func instantiateCoreWithModuleLease(c *Compiled, opts InstantiateOptions, moduleUse *Module) (*Instance, error) {',after='\nprobeStart0 := probetime.Now()')
insert('return b.instantiate()',before='lifecycleProbeEnd(0, probeStart0)\n')
insert('func (b *instanceBuilder) instantiate() (result *Instance, err error) {',after='\nprobeStart1 := probetime.Now()')
insert('eng, err := runtime.AcquireEngineWithStackBytes(stackBytes)',before='lifecycleProbeEnd(1, probeStart1)\nprobeStart2 := probetime.Now()\n',after='\nlifecycleProbeEnd(2, probeStart2)')
span(3,'// Memory: a host-imported *Memory if the module imports one, otherwise an','// Release every owned mapping once and detach every distinct imported memory')
insert('ar, err := runtime.AcquireArena(arenaNeed)',before='probeStart4 := probetime.Now()\n',after='\nlifecycleProbeEnd(4, probeStart4)')
span(5,'var globals []byte','var gcRefTestTable *gcRefTestTableState')
span(6,'var gcRefTestTable *gcRefTestTableState','var gcArrayElements *gcArrayElementState')
span(7,'if initErr == nil && len(c.Data) > 0 {','argsBytes, err := runtime.SlotBytes(c.maxParamSlots)')
insert('if c.HasStart {',before='probeStart8 := probetime.Now()\n')
insert('\tb.success = true\n\treturn in, nil',before='if c.HasStart { lifecycleProbeEnd(8, probeStart8) }\n')
source+='''
var lifecycleProbeTotals [10]probeatomic.Int64
func lifecycleProbeEnd(phase int, start probetime.Time) { lifecycleProbeTotals[phase].Add(probetime.Since(start).Nanoseconds()) }
'''
(TMP/'instantiate-probe.go').write_text(source)
fixtures=json.loads((ROOT/'corpus/catalog.json').read_text())['benchmarks']
entries='\n'.join('{"'+m['id']+'", "../../corpus/'+m['artifact']+'"},' for m in fixtures if m['id'] in ['tiny','utf8proc','pcre2','xxhash'])
test='''package wago
import ("os"; "testing"; "time"; "github.com/wago-org/wago/tests/support/wasmtest")
func BenchmarkInstancePhaseProbe(b *testing.B) {
 cases := []struct{name,path string}{ENTRIES}
 cases=append(cases,struct{name,path string}{name:"synthetic-start"})
 for _,tc:=range cases { b.Run(tc.name,func(b *testing.B){
 var data []byte
 var err error
 if tc.path!="" {data,err=os.ReadFile(tc.path);if err!=nil {b.Fatal(err)}} else {
 data=wasmtest.Module(wasmtest.Section(1,[]byte{1,0x60,0,0}),wasmtest.Section(3,[]byte{1,0}),wasmtest.Section(5,[]byte{1,1,1,1}),wasmtest.Section(8,[]byte{0}),wasmtest.Section(10,wasmtest.Vec(wasmtest.Code([]byte{0x41,0,0x41,7,0x3a,0,0,0x0b}))))
 }
 c,err:=Compile(nil,data);if err!=nil {b.Fatal(err)};defer c.Close()
 warm,err:=Instantiate(c);if err!=nil {b.Fatal(err)};if err:=warm.Close();err!=nil {b.Fatal(err)}
 for i:=range lifecycleProbeTotals {lifecycleProbeTotals[i].Store(0)}
 b.ReportAllocs();b.ResetTimer()
 for i:=0;i<b.N;i++ {
 in,err:=Instantiate(c);if err!=nil {b.Fatal(err)}
 if c.HasStart && in.Memory().UnsafeBytes()[0]!=7 {in.Close();b.Fatal("start did not initialize memory")}
 started:=time.Now();err=in.Close();lifecycleProbeEnd(9,started);if err!=nil {b.Fatal(err)}
 }
 b.StopTimer()
 for i,name:=range []string{"public-preparation","builder-preparation","engine-acquire","memory-acquire","arena-acquire","globals","tables","active-data","start","close"} {b.ReportMetric(float64(lifecycleProbeTotals[i].Load())/float64(b.N),name+"-ns/op")}
 }) }
}
'''.replace('ENTRIES',entries)
(TMP/'instance-phase-probe_test.go').write_text(test)
replace={str(ROOT/'src/wago/instantiate.go'):str(TMP/'instantiate-probe.go'),str(ROOT/'src/wago/instance_phase_probe_test.go'):str(TMP/'instance-phase-probe_test.go')}
for label in ['baseline','candidate']:
 mapping=dict(replace)
 if label=='baseline':mapping[str(ROOT/'src/wago/imports.go')]=str(TMP/'imports-baseline.go')
 (TMP/(label+'-phase-overlay.json')).write_text(json.dumps({'Replace':mapping}))
print('Generated test-only overlays; instrumented results must not be used as final timings.')
