#!/usr/bin/env python3
"""Fetch only the bounded pinned Wasm-R3 set, hash, and scan exact sum shape.
No build, Wasm execution, or native execution occurs. Run with Python 3.
"""
import hashlib,json,pathlib,re,urllib.request
REV='bea10061c81428260ff029a9953273540a0c32e2'
TREE=pathlib.Path(__file__).resolve().parent/'results/followup/r3-tree.json'
import argparse
p=argparse.ArgumentParser()
p.add_argument('--out',required=True)
a=p.parse_args()
OUT=pathlib.Path(a.out)
if OUT.exists():raise SystemExit('Use a new output directory')
OUT.mkdir(exist_ok=True)
U=rb'[\x80-\xff]*[\x00-\x7f]'
PATTERN=re.compile(rb'\x20(?P<count>'+U+rb')\x45\x0d\x01\x20(?P<acc>'+U+rb')\x20(?P<addr>'+U+rb')\x29'+U+rb'\x00\x7c\x21(?P=acc)\x20(?P=addr)\x41\x08\x6a\x21(?P=addr)\x20(?P=count)\x41\x01\x6b\x21(?P=count)\x0c\x00\x0b\x0b')
TOYS={'factorial.wasm','fib.wasm','multiplyInt.wasm','multiplyDouble.wasm','mandelbrot.wasm'}
def leb(b,o):
    value=shift=0
    while True:
        x=b[o];o+=1;value|=(x&127)<<shift
        if x<128:return value,o
        shift+=7
        if shift>63:raise ValueError('LEB length exceeds limit')
def scan(b):
    if b[:8]!=b'\x00asm\x01\x00\x00\x00':raise ValueError('not a Wasm v1 module')
    offset=8;functions=0;matches=[]
    while offset<len(b):
        section=b[offset];size,start=leb(b,offset+1);end=start+size
        if end>len(b):raise ValueError('section exceeds module')
        offset=end
        if section!=10:continue
        n,pos=leb(b,start)
        for fi in range(n):
            size,body_start=leb(b,pos);body=b[body_start:body_start+size];pos=body_start+size;functions+=1
            if pos>end:raise ValueError('function exceeds code section')
            for m in PATTERN.finditer(body):
                matches.append({'local_function_index':fi,'body_offset':m.start(),'module_offset':body_start+m.start(),'locals':{k:leb(v,0)[0] for k,v in m.groupdict().items()}})
    return functions,matches
j=json.loads(TREE.read_text())
selected=[x for x in j['tree'] if x['type']=='blob' and x['path'].startswith('wasm-r3-bench/') and x['path'].endswith('.wasm') and x['size']<2*1024*1024]
assert j['sha']==REV and len(selected)<=21
rows=[]
for item in selected:
    name=pathlib.Path(item['path']).name
    url=f'https://raw.githubusercontent.com/doehyunbaek/wasm-benchmarks/{REV}/{item["path"]}'
    target=OUT/name
    row={'path':item['path'],'url':url,'git_blob':item['sha'],'listed_bytes':item['size'],'arithmetic_toy_excluded_from_application_claim':name in TOYS}
    try:
        if target.exists():b=target.read_bytes()
        else:
            with urllib.request.urlopen(url,timeout=30) as response:b=response.read(2*1024*1024+1)
            if len(b)!=item['size']:raise ValueError('download size mismatch')
            target.write_bytes(b)
        if len(b)!=item['size']:raise ValueError('cached size mismatch')
        if hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest()!=item['sha']:raise ValueError('Git blob mismatch')
        row['sha256']=hashlib.sha256(b).hexdigest();row['bytes']=len(b)
        row['functions'],row['candidate_matches']=scan(b)
    except Exception as e:row['error']=str(e)
    rows.append(row)
    print(name,row.get('sha256'),row.get('functions'),'matches',len(row.get('candidate_matches',[])),row.get('error',''),flush=True)
report={'revision':REV,'tree_source':str(TREE),'modules_selected':len(rows),'bytes_selected':sum(x['listed_bytes'] for x in rows),'rows':rows,'limitations':[
    'Conservative byte-pattern scan of code sections. Matches would need instruction-boundary, local-type, control-shape, compiler-feature, and register-pin verification.',
    'The fixed zero offset, stride 8, decrement 1, and branch labels use canonical LEB encodings in this scan; noncanonical fixed immediates are not matched.',
    'No build, Wago compilation, replay, or native execution occurred. Runtime diagnostic admission is unverified.',
    'Replay names do not establish workload provenance, correctness oracle, meaningful work count, or redistribution rights. No eligible real-application claim is made from names.',
    'Only the 21 modules below 2 MiB were fetched. Larger modules were excluded. Arithmetic toy names are explicitly excluded from application claims.'
]}
(OUT/'scan.json').write_text(json.dumps(report,indent=2)+'\n')
print('report',OUT/'scan.json')
