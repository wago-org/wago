#!/usr/bin/env python3
"""Bounded fixed-work memory processes; GC and scavenging use separate processes."""
import argparse
import json
import os
from pathlib import Path
import re
import select
import subprocess
import sys
import time
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from measurement import ROOT,capture,digest,executable,fresh_directory,host,identity,settings


def proc(pid, detailed=False):
    root=Path(f'/proc/{pid}')
    fields={line.split(':')[0]:line.split(':')[1].strip() for line in (root/'status').read_text().splitlines() if ':' in line}
    stat=(root/'stat').read_text().split(') ',1)[1].split()
    result={key:int(fields[key].split()[0]) for key in ['VmRSS','VmSize','VmHWM','VmSwap','Threads'] if key in fields}
    result.update(minor_faults=int(stat[7]),major_faults=int(stat[9]))
    if not detailed:return result
    result['descriptors']=len(list((root/'fd').iterdir()))
    result['mappings']={}
    current=None
    for line in (root/'smaps').read_text().splitlines():
        if re.match(r'^[0-9a-f]+-[0-9a-f]+ ',line):
            f=line.split();perms=f[1];name=f[5] if len(f)>5 else ''
            key='stack' if name.startswith('[stack') else 'file' if name.startswith('/') else 'anonymous-exec' if 'x' in perms else 'anonymous-none' if perms.startswith('---') else 'anonymous-rw' if not name else 'other'
            current=result['mappings'].setdefault(key,dict(count=0,Size=0,Rss=0,Pss=0,Private_Dirty=0,Anonymous=0,Swap=0))
            current['count']+=1
        elif current is not None and ':' in line:
            key,value=line.split(':',1)
            if key in current and key!='count': current[key]+=int(value.split()[0])
    result['Pss']=sum(v['Pss'] for v in result['mappings'].values())
    return result


def one(binary, path, env, prefix, module, api, intervention, commands, epochs, profile=False, legacy=False, mapping_snapshot=False, warm=1, callers=1, workers=1):
    notify_r,notify_w=os.pipe();ack_r,ack_w=os.pipe()
    child_env=dict(env)
    if not legacy: child_env.update(WAGO_MEMORY_REVIEW='1',WAGO_REVIEW_NOTIFY_FD=str(notify_w),WAGO_REVIEW_ACK_FD=str(ack_r))
    command=prefix+[str(binary),'-test.run',f'^TestWASIResources$/^{module}$' if legacy else '^TestWASIMemoryReview$','-test.count','1','-test.v','-wago.corpus','cjson,tinyxml2' if legacy else module if module not in ['minimal-wasi','host1024','startup'] else 'cjson','-wago.bench.lifecycle']
    if not legacy: command+=['-wago.review.module',module,'-wago.review.api',api,'-wago.review.intervention',intervention,'-wago.review.commands',str(commands),'-wago.review.warm',str(warm),'-wago.review.epochs',str(epochs)]
    if api=='compile': command+=['-wago.review.callers',str(callers),'-wago.review.workers',str(workers)]
    if profile:
        child_env['GODEBUG']='memprofilerate=1,gctrace=1,inittrace=1'
        command+=['-wago.review.profile',str(path.with_suffix('.heap'))]
    start=time.monotonic();before=host();points=[];interval_peak=0;peak=0;p=None;area=0;duration=0;observations=0;last_time=None;last_rss=0;exec_hwm=0
    try:
        with path.open('x') as log:
            p=subprocess.Popen(command,cwd=ROOT/'bench/suite',env=child_env,stdout=log,stderr=subprocess.STDOUT,pass_fds=(notify_w,ack_r))
            os.close(notify_w);notify_w=-1;os.close(ack_r);ack_r=-1
            while True:
                pid,status,usage=os.wait4(p.pid,os.WNOHANG)
                if pid:
                    p.returncode=os.waitstatus_to_exitcode(status);break
                if time.monotonic()-start>60:
                    p.kill();p.wait();raise ValueError('memory process exceeded 60s limit')
                try:
                    sample=proc(p.pid);exec_hwm=max(exec_hwm,sample.get('VmHWM',0));rss=sample.get('VmRSS',0);peak=max(peak,rss);interval_peak=max(interval_peak,rss)
                    now=time.monotonic()
                    if last_time is not None: area+=last_rss*(now-last_time);duration+=now-last_time
                    last_time=now;last_rss=rss;observations+=1
                    if rss>512*1024:
                        p.kill();p.wait();raise ValueError('memory process exceeded 512 MiB RSS limit')
                except FileNotFoundError: pass
                readable,_,_=select.select([notify_r],[],[],.005)
                if readable:
                    message=os.read(notify_r,1)
                    if message:
                        observation=time.monotonic()
                        sample=proc(p.pid,True)
                        exec_hwm=max(exec_hwm,sample.get('VmHWM',0))
                        sample.update(index=message[0],elapsed_seconds=observation-start,observation_seconds=time.monotonic()-observation,preceding_interval_sampled_peak_rss_kib=max(interval_peak,sample.get('VmRSS',0)))
                        sample.update(sampled_rss_area_kib_seconds=area,sampled_seconds=duration,sample_count=observations,sustained_rss_kib=area/duration if duration else None)
                        points.append(sample);interval_peak=0;area=0;duration=0;observations=0;last_time=None
                        if profile or mapping_snapshot:
                            path.with_suffix(f'.phase{message[0]}.smaps').write_text(Path(f'/proc/{p.pid}/smaps').read_text())
                        os.write(ack_w,message)
                    else: time.sleep(.005)
    finally:
        if p is not None and p.returncode is None:
            p.kill()
            p.wait()
        for fd in [notify_r,notify_w,ack_r,ack_w]:
            if fd>=0:os.close(fd)
    text=path.read_text()
    if p.returncode or not re.search(r'^PASS$',text,re.M):raise ValueError(f'failed memory process: {path}')
    if legacy:
        records=[json.loads(line[line.index('{"'):]) for line in text.splitlines() if '{"' in line]
        if len(records)!=4:raise ValueError('missing historical resource checkpoints')
        data={'legacy':records}
    else:
        lines=[line[len('MEMORY_REVIEW '):] for line in text.splitlines() if line.startswith('MEMORY_REVIEW ')]
        if len(lines)!=1:raise ValueError('missing memory result')
        data=json.loads(lines[0])
        expected=['startup','ready','setup','warm']+[f'epoch{i+1}' for i in range(epochs)]+['released','post']
        if [r['Phase'] for r in data['Points']]!=expected or len(points)!=len(expected) or [r['index'] for r in points]!=list(range(len(expected))):raise ValueError('missing or duplicate checkpoints')
        for r,external in zip(data['Points'],points):r['external']=external
    return dict(command=command,environment={k:child_env[k] for k in ['GOMAXPROCS','GOGC','GOMEMLIMIT','GODEBUG','WAGO_BOUNDS']},seconds=time.monotonic()-start,host_before=before,host_after=host(),whole_process_max_rss_kib=usage.ru_maxrss,postexec_hwm_kib=exec_hwm,sampled_peak_rss_kib=peak,minor_faults=usage.ru_minflt,major_faults=usage.ru_majflt,result=data)


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--binaries',required=True,type=Path)
    p.add_argument('--output',required=True,type=Path)
    p.add_argument('--samples',type=int,default=20)
    p.add_argument('--modules',nargs='+',default=['minimal-wasi','cjson','tinyxml2'])
    p.add_argument('--apis',nargs='+',choices=['raw','provider','compile'],default=['raw','provider'])
    p.add_argument('--interventions',nargs='+',choices=['normal','gc','scavenge'],default=['normal','gc','scavenge'])
    p.add_argument('--commands',type=int,default=1000)
    p.add_argument('--epochs',type=int,default=1)
    p.add_argument('--warm',type=int,choices=[0,1],default=1)
    p.add_argument('--callers',type=int,choices=[1,4,16],default=1)
    p.add_argument('--workers',type=int,choices=[0,1,2,4,8],default=1)
    p.add_argument('--profile',action='store_true')
    p.add_argument('--legacy',action='store_true')
    p.add_argument('--mapping-snapshot',action='store_true',help='separate detailed smaps diagnostic; no Go profiling')
    p.add_argument('--gomaxprocs',type=int,default=16)
    p.add_argument('--cpus',default='0-15')
    a=p.parse_args()
    if any(len(v)!=len(set(v)) for v in [a.modules,a.apis,a.interventions]):p.error('duplicate experiment cases')
    if not 0<=a.commands<=10000 or not 1<=a.epochs<=4 or a.samples<1:p.error('invalid bounded work or sample count')
    if a.legacy and (a.apis!=['raw'] or a.interventions!=['scavenge'] or a.profile or a.commands!=1000 or a.epochs!=1):p.error('legacy requires --apis raw --interventions scavenge, no profiling')
    binaries={k:executable(a.binaries/f'wasi-{k}.test') for k in ['baseline','candidate']}
    build=json.loads((a.binaries/'build.json').read_text())
    for k,v in binaries.items():
        if digest(v)!=build['binaries'][k]['sha256']:raise ValueError('binary hash mismatch')
    out=fresh_directory(a.output);env,prefix=settings(a.gomaxprocs,a.cpus)
    meta=identity(env,binaries);meta.update(build=build,configuration=vars(a)|{'binaries':str(a.binaries),'output':str(out)},sample_interval_seconds=.005,status='running',records=[])
    (out/'run.json').write_text(json.dumps(meta,indent=2)+'\n')
    for pair in range(1,a.samples+1):
        for label in (['baseline','candidate'] if pair%2 else ['candidate','baseline']):
            for module in a.modules:
                for api in a.apis:
                    for intervention in a.interventions:
                        name=f'{pair:03d}-{label}-{module}-{api}-{intervention}'
                        record=one(binaries[label],out/f'{name}.txt',env,prefix,module,api,intervention,a.commands,a.epochs,a.profile,a.legacy,a.mapping_snapshot,a.warm,a.callers,a.workers)
                        record.update(pair=pair,label=label,module=module,api=api,intervention=intervention,run_id=meta['run_id'])
                        result=out/f'{name}.json';result.write_text(json.dumps(record,indent=2)+'\n')
                        meta['records'].append({'file':result.name,'sha256':digest(result)})
        (out/'run.json').write_text(json.dumps(meta,indent=2)+'\n')
        print(f'{out.name}: pair {pair}/{a.samples}',flush=True)
    meta['status']='complete';(out/'run.json').write_text(json.dumps(meta,indent=2)+'\n')


if __name__=='__main__':main()
