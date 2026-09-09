import fs from 'node:fs';
const out='/home/jtenner/Projects/wago/.tmp/pr564-memory/final-closures/host.jsonl';
const sample=()=>fs.appendFileSync(out,JSON.stringify({time:new Date().toISOString(),boot:fs.readFileSync('/proc/sys/kernel/random/boot_id','utf8').trim(),load:fs.readFileSync('/proc/loadavg','utf8').trim(),cpu:fs.readFileSync('/proc/stat','utf8').split('\n').filter(s=>/^cpu/.test(s)),mem:fs.readFileSync('/proc/meminfo','utf8').split('\n').filter(s=>/^(MemTotal|MemAvailable|SwapTotal|SwapFree):/.test(s))})+'\n');
sample();setInterval(sample,10000);
