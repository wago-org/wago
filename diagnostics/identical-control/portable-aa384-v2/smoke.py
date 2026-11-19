#!/usr/bin/env python3
"""Exactly four untimed-for-analysis correctness probes, no A/A run."""
import argparse,json,os,subprocess,tempfile
from pathlib import Path
from prepare import KIT, read_checked, require

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--manifest',type=Path,required=True)
    p.add_argument('--manifest-sha256',required=True)
    p.add_argument('--new-receipt',type=Path,required=True)
    p.add_argument('--execute-four-smoke-only',action='store_true',required=True)
    a=p.parse_args();m=read_checked(a.manifest,a.manifest_sha256)
    with a.new_receipt.open('x') as log,tempfile.TemporaryDirectory() as temp:
        def record(x):
            log.write(json.dumps(x)+'\n');log.flush();os.fsync(log.fileno())
        record({'stage':'start','manifest_sha256':a.manifest_sha256,'scope':'four correctness probes; no timing analysis'})
        env={'PATH':os.defpath,'HOME':temp,'TMPDIR':temp,'LANG':'C','GOMAXPROCS':'1',**json.loads((KIT/'flags.json').read_text())}
        for w in json.loads((KIT/'inputs.json').read_text()):
            record({'stage':'case_start','id':w['id']})
            with tempfile.TemporaryFile() as errs:
                proc=subprocess.Popen([m['taskset'],'-c',str(m['cpu']),m['binary']],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=errs,text=True,encoding='utf-8',env=env)
                def ask(i,method,**fields):
                    proc.stdin.write(json.dumps({'version':1,'id':i,'method':method,**fields})+'\n');proc.stdin.flush()
                    phases=[]
                    while True:
                        line=proc.stdout.readline()
                        require(bool(line),'adapter EOF')
                        r=json.loads(line)
                        require(r.get('id')==i,'adapter response ID mismatch')
                        if r.get('status')=='phase':
                            require(method=='run','unexpected phase outside compile')
                            phases.append(r['phase']['stage'])
                            proc.stdin.write(json.dumps({'version':1,'id':i,'method':'continue'})+'\n');proc.stdin.flush()
                            continue
                        require(r.get('status')=='ok','adapter response error: '+str(r.get('error',r.get('status'))))
                        if method=='run':
                            require(phases==['before_compile','compiled','released'],'phase order mismatch')
                        return r
                try:
                    ask(1,'describe')
                    ask(2,'prepare',prepare={'artifact':str(Path(m['corpus'])/w['artifact']),'artifact_sha256':w['sha256'],'workload':w,'profile':'timing'})
                    result=ask(3,'run',run={'scenario':'compile','samples':1,'operations':1,'warmup':0,'phase_barriers':True})
                    require(len(result['samples'])==1 and result['samples'][0]['verified'],'oracle mismatch')
                    ask(4,'close');proc.stdin.close();require(proc.wait(timeout=30)==0,'adapter exit failed')
                    record({'stage':'case_complete','id':w['id'],'oracle_verified':True})
                finally:
                    if proc.poll() is None:proc.kill();proc.wait()
        record({'stage':'complete','verified':4})
if __name__=='__main__':main()
