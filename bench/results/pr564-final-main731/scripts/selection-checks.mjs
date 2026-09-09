import assert from 'node:assert/strict';
import fs from 'node:fs';
import {materialMemoryWarning} from './selection.mjs';
const cases=[
 ['new large byte cost',{base:0,candidate:131072,unit:'B/op',delta:null,p:null},true],
 ['below absolute byte gate',{base:0,candidate:131071,unit:'B/op',delta:null,p:null},false],
 ['new large allocation count',{base:0,candidate:128,unit:'allocs/op',delta:null,p:null},true],
 ['new significant eight allocations',{base:0,candidate:8,unit:'allocs/op',delta:null,p:.002},true],
 ['seven allocations',{base:0,candidate:7,unit:'allocs/op',delta:null,p:.002},false],
 ['ordinary significant byte increase',{base:65536,candidate:66000,unit:'B/op',delta:.708,p:.01},true],
 ['small byte increase',{base:65536,candidate:65800,unit:'B/op',delta:.403,p:.01},false],
 ['decrease',{base:200000,candidate:0,unit:'B/op',delta:-100,p:.001},false],
 ['execution counter',{name:'BenchmarkExec/fib',base:0,candidate:1000000,unit:'B/op',delta:null,p:null},false],
 ['Wazero control',{name:'BenchmarkWazeroCompile/fib',base:0,candidate:1000000,unit:'B/op',delta:null,p:null},false],
];
for(const[name,row,expected]of cases)assert.equal(materialMemoryWarning({name:'BenchmarkCompileFull/fixture',...row}),expected,name);
const result=`${cases.length} explicit memory-selection cases: PASS\n`;
fs.writeFileSync('/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures/selection-checks.txt',result);
console.log(result.trim());
