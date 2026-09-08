import fs from 'node:fs';
const out='/tmp/wago-pr564-regressions-zHTLww/host.jsonl';
function sample() {
 fs.appendFileSync(out,JSON.stringify({time:new Date().toISOString(),load:fs.readFileSync('/proc/loadavg','utf8').trim(),cpuPressure:fs.readFileSync('/proc/pressure/cpu','utf8').trim(),memory:fs.readFileSync('/proc/meminfo','utf8').split('\n').filter(s=>/MemAvailable|SwapFree|MemTotal/.test(s))})+'\n');
}
sample();setInterval(sample,5000);
