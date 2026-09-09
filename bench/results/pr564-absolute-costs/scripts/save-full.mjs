import fs from 'node:fs';
const root='/home/jtenner/Projects/wago/.tmp/pr564-absolute-costs',out='/home/jtenner/Projects/wago/bench/results/pr564-absolute-costs';
fs.mkdirSync(out,{recursive:true});
for(const name of ['all-metrics-main-vs-after.csv','all-timings-main-vs-after.csv','timings-explicit-main-vs-after.md','timings-signals-main-vs-after.md','listed-full.md','application-timings-by-main-time.md'])fs.copyFileSync(`${root}/rendered/${name}`,`${out}/${name}`);
for(const name of ['listed-focused.md','focused-main-vs-after.csv','confirmed-timing-regressions.md'])if(fs.existsSync(`${root}/rendered/${name}`))fs.copyFileSync(`${root}/rendered/${name}`,`${out}/${name}`);
