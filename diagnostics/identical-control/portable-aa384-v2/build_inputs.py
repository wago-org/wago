#!/usr/bin/env python3
"""Opt-in offline preparation of v2 adapter and exact tracked corpus bytes."""
import argparse, hashlib, json, os, platform, shutil, subprocess, sys
from pathlib import Path
from prepare import KIT, require, sha

WAGO = 'b5ab03f924e6b74ee3b558e34ad9cb32c9026acd'
WASMBENCH = 'ca62116e0df59639e968f371ec0025395aabbef0'

def main():
    p=argparse.ArgumentParser(description=__doc__)
    for k in ('wago','wasmbench','go','gomodcache','gocache','new-output-directory'):
        p.add_argument('--'+k, type=Path, required=True)
    p.add_argument('--build-local-v2', action='store_true', required=True)
    a=p.parse_args()
    require(sys.version_info >= (3,10) and sys.flags.optimize == 0,'Python >=3.10, no -O')
    require(platform.system()=='Linux' and platform.machine()=='x86_64','Linux x86-64 required')
    a.wago=a.wago.resolve(strict=True);a.wasmbench=a.wasmbench.resolve(strict=True)
    a.go=a.go.resolve(strict=True);a.gomodcache=a.gomodcache.resolve(strict=True);a.gocache=a.gocache.resolve()
    out=a.new_output_directory.resolve()
    require(not any(out.is_relative_to(x) or a.gocache.is_relative_to(x) for x in (a.wago,a.wasmbench,KIT)), 'output and writable cache must be outside sources and kit')
    git=shutil.which('git')
    require(git is not None,'Git required')
    env={'PATH':os.defpath,'HOME':str(Path.home()),'GOENV':'off','GOWORK':'off','GOTOOLCHAIN':'local',
         'GOTELEMETRY':'off','GOPROXY':'off','GOSUMDB':'off','GOVCS':'*:off','CGO_ENABLED':'0',
         'GOOS':'linux','GOARCH':'amd64','GOAMD64':'v1','GOMAXPROCS':'1','GOMODCACHE':str(a.gomodcache),'GOCACHE':str(a.gocache)}
    def clean(path, pin):
        def q(*args):return subprocess.check_output([git,'-C',str(path),*args],env=env,text=True).strip()
        require(q('rev-parse','HEAD')==pin and not q('status','--porcelain'),'source not clean/pinned: '+str(path))
    clean(a.wago,WAGO);clean(a.wasmbench,WASMBENCH)
    env['GIT_CONFIG_NOSYSTEM']='1'
    version=subprocess.check_output([str(a.go),'version'],env=env,text=True).strip()
    require(version=='go version go1.27.1 linux/amd64', 'Go 1.27.1 linux/amd64 required')
    inputs=json.loads((KIT/'inputs.json').read_text())
    data={}
    for w in inputs:
        artifact=w['original_contract']['artifact']
        blob=subprocess.check_output([git,'-C',str(a.wago),'show',WAGO+':corpus/'+artifact],env=env)
        require(hashlib.sha256(blob).hexdigest()==w['sha256'],'tracked input SHA mismatch: '+w['id'])
        data[w['artifact']]=blob
    out.mkdir(mode=0o700,exist_ok=False)
    (out/'corpus').mkdir()
    (out/'tmp').mkdir()
    env['TMPDIR']=str(out/'tmp')
    for name,blob in data.items():(out/'corpus'/name).write_bytes(blob)
    # Go module syntax accepts quoted paths. JSON quoting covers spaces and
    # embedded quotes without interpolating user paths in a shell command.
    (out/'adapter.mod').write_text((KIT/'go.mod.template').read_text().replace('@WAGO@',json.dumps(str(a.wago))).replace('@WASMBENCH@',json.dumps(str(a.wasmbench))))
    shutil.copyfile(KIT/'go.sum.pinned',out/'adapter.sum')
    binary=out/'adapter'
    command=[str(a.go),'build','-p','1','-pgo=off','-trimpath','-buildvcs=false','-mod=readonly','-modfile='+str(out/'adapter.mod'),
             '-ldflags=-R 4096 -T 0x402000 -X main.sourceRevision='+WAGO,'-o',str(binary),'.']
    (out/'BUILD-START.json').write_text(json.dumps({'command':command,'go':version,'go_tool_sha256':sha(a.go),
        'source':{'wago':WAGO,'wasmbench':WASMBENCH},'adapter_mod_sha256':sha(out/'adapter.mod'),
        'adapter_sum_sha256':sha(out/'adapter.sum'),'policy': 'v2 source-built; CGO disabled; no historical binary parity claim'},indent=2)+'\n')
    with (out/'build.log').open('x') as f:
        subprocess.run(command,cwd=a.wasmbench/'adapters/wago',env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
    clean(a.wago,WAGO);clean(a.wasmbench,WASMBENCH)
    info=subprocess.check_output([str(a.go),'version','-m',str(binary)],env=env,text=True)
    require('go1.27.1' in info and '\tGOARCH=amd64' in info and '\tGOOS=linux' in info,'unexpected adapter build metadata')
    (out/'BUILD-RESULT.json').write_text(json.dumps({'adapter_sha256':sha(binary),'version_m':info,
      'adapter_sum_sha256':sha(out/'adapter.sum'),'inputs':{w['id']:w['sha256'] for w in inputs}},indent=2)+'\n')
    print(sha(binary))
if __name__=='__main__':main()
