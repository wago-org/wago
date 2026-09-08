import fs from 'node:fs';
const out='/tmp/wago-pr564-HbN430/host-load.jsonl';
const sample=()=>{
  const memory=fs.readFileSync('/proc/meminfo','utf8');
  const row={utc:new Date().toISOString(),load:fs.readFileSync('/proc/loadavg','utf8').trim(),memAvailableKiB:Number(memory.match(/^MemAvailable:\s+(\d+)/m)?.[1]),swapFreeKiB:Number(memory.match(/^SwapFree:\s+(\d+)/m)?.[1]),pressure:fs.readFileSync('/proc/pressure/memory','utf8').trim()};
  fs.appendFileSync(out,JSON.stringify(row)+'\n');
};
sample();
const interval=setInterval(sample,30000);
for(const signal of ['SIGINT','SIGTERM'])process.on(signal,()=>{sample();clearInterval(interval);process.exit(0);});
