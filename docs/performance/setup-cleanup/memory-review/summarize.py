#!/usr/bin/env python3
"""Validate a fixed-work run and report paired differences without mixing sessions."""
import argparse
import json
from pathlib import Path
import random
import statistics as st
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from measurement import digest

PRIMARY=['HeapAlloc','HeapInuse','HeapReleased','RSS_KiB','PSS_KiB']


def interval(values):
    rng=random.Random(67221)
    n=len(values)
    samples=sorted(st.fmean(values[rng.randrange(n)] for _ in range(n)) for _ in range(10000))
    return [samples[249],samples[9749]]


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('run',type=Path);p.add_argument('--output',required=True,type=Path);a=p.parse_args()
    meta=json.loads((a.run/'run.json').read_text());cfg=meta['configuration']
    if meta['status']!='complete':raise ValueError('incomplete run')
    expected={(pair,label,module,api,mode) for pair in range(1,cfg['samples']+1) for label in ['baseline','candidate'] for module in cfg['modules'] for api in cfg['apis'] for mode in cfg['interventions']}
    seen=set();groups={};hashes={};invariants=[]
    for entry in meta['records']:
        path=a.run/entry['file']
        if path.parent.resolve()!=a.run.resolve() or digest(path)!=entry['sha256']:raise ValueError('changed result')
        r=json.loads(path.read_text());key=tuple(r[k] for k in ['pair','label','module','api','intervention'])
        if key not in expected or key in seen or r['run_id']!=meta['run_id']:raise ValueError('duplicate or mixed process')
        seen.add(key)
        if r['environment']['GOMAXPROCS']!=meta['environment']['GOMAXPROCS'] or r['environment']['GOGC']!='100':raise ValueError('mixed runtime configuration')
        if cfg['legacy']:
            for point in r['result']['legacy']:
                import re
                v={'HeapAlloc':point['heap_alloc'],'HeapInuse':point['heap_inuse'],'RSS_KiB':int(re.search(r'^Rss:\s+(\d+)',point['smaps'],re.M)[1])}
                groups.setdefault((r['module'],r['api'],r['intervention'],point['phase']),{}).setdefault(r['label'],{})[r['pair']]=v
            continue
        result=r['result']
        if result['Commands']!=cfg['commands'] or result['Epochs']!=cfg['epochs'] or result['API']!=r['api'] or result['Module']!=r['module'] or result['Intervention']!=r['intervention']:raise ValueError('mixed workload')
        if r['module'] in hashes and hashes[r['module']]!=result['SHA256']:raise ValueError('mixed module bytes')
        hashes[r['module']]=result['SHA256']
        for point in result['Points']:
            v={k:value for k,value in point.items() if isinstance(value,(int,float))}
            v.update(RSS_KiB=point['external']['VmRSS'],PSS_KiB=point['external']['Pss'],Descriptors=point['external']['descriptors'],NativeActive=point['Native']['Active'],NativeCached=point['Native']['Cached'])
            for i,name in enumerate(['LiveHeap','GCGoal','MetricObjects','MetricReleased']):
                if point['MetricAvailable'][i]:v[name]=point['Metrics'][i]
            for kind,values in point['external']['mappings'].items():
                for metric in ['count','Size','Rss','Pss','Private_Dirty','Swap']:v[f'map_{kind}_{metric}']=values[metric]
            v['IntervalPeakRSS_KiB']=point['external']['preceding_interval_sampled_peak_rss_kib']
            groups.setdefault((r['module'],r['api'],r['intervention'],point['Phase']),{}).setdefault(r['label'],{})[r['pair']]=v
        points={v['Phase']:v for v in result['Points']}
        if points['released']['Native']['Active']!=0:invariants.append([key,'active native memory after cleanup'])
        if points['released']['external']['descriptors']>points['warm']['external']['descriptors']:invariants.append([key,'descriptor growth'])
    if seen!=expected:raise ValueError('missing samples')
    output={'run_id':meta['run_id'],'n_pairs':cfg['samples'],'estimate':'difference of means with paired bootstrap 95% interval; medians separately','invariant_failures':invariants,'groups':{}}
    for key,labels in sorted(groups.items()):
        base=labels['baseline'];candidate=labels['candidate'];names=set.intersection(*(set(v) for v in [*base.values(),*candidate.values()]))
        target={}
        for metric in sorted(names):
            b=[base[i][metric] for i in range(1,cfg['samples']+1)];c=[candidate[i][metric] for i in range(1,cfg['samples']+1)];d=[y-x for x,y in zip(b,c)]
            target[metric]={'baseline_median':st.median(b),'candidate_median':st.median(c),'baseline_mean':st.fmean(b),'candidate_mean':st.fmean(c),'paired_mean_delta':st.fmean(d),'relative_mean_percent':100*st.fmean(d)/st.fmean(b) if st.fmean(b) else None}
            if metric in PRIMARY and (key[-1] in ['released','post','released-normal','released-gc','closed-normal']):target[metric]['paired_mean_delta_95ci']=interval(d)
        output['groups']['/'.join(key)]=target
    with a.output.open('x') as f:json.dump(output,f,indent=2);f.write('\n')
    if invariants:raise ValueError(f'ownership invariant failures: {invariants}')
    print(f'{cfg["samples"]} complete pairs; {len(groups)} groups; no descriptor/native-active growth')


if __name__=='__main__':main()
