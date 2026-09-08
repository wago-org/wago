import fs from 'node:fs';
const out='/tmp/wago-pr564-HbN430';
const rows=fs.readdirSync(out).filter(n=>/^test-.*\.status\.json$/.test(n)).sort().map(file=>({file,...JSON.parse(fs.readFileSync(`${out}/${file}`))}));
const text=`# Test command results

This table includes retained failed attempts, not just successful reruns.
See REPORT.md for source pins and the final qualification limits. Exit zero
means this command passed; a focused command does not qualify a full platform.

| Capture | Exit | Wrapper seconds |
|---|---:|---:|
${rows.map(r=>`| ${r.file} | ${r.code} | ${r.seconds??''} |`).join('\n')}

The final full pinned-tool root run fails on two Wine integration tests. Other
packages pass. Compiler/runtime tests also pass natively with Go 1.27.1. ARM64
runtime checks use QEMU, not native ARM64 hardware. The pinned standalone
package passes with TinyGo 0.41.1 and Go 1.22.12. Build-profile smoke results
and the Wine certutil diagnostic are separate JSON captures.
`;
fs.writeFileSync(`${out}/test-summary.md`,text);
console.log(`${rows.length} retained test status captures.`);
