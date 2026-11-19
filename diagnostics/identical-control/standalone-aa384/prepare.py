#!/usr/bin/env python3
"""Prepare or check an explicitly selected local diagnostic host; never run adapter."""
import argparse, hashlib, json, os, platform, re, shutil, stat, struct, subprocess, sys
from pathlib import Path

KIT = Path(__file__).resolve().parent
PACKAGE = ('prepare.py', 'run.py', 'analyze.py', 'inputs.json', 'flags.json', 'README.md', 'PROVENANCE.json')
EXPECTED_INPUTS = '66a3f1d2277b09b1b998f4ed2c14a0cfefe759f9ab4f9c1d232968290955ebe3'
def require(test, message):
    if not test:
        raise RuntimeError(message)
def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()
def attest(binary, corpus, cpu, go, expected_sha, expected_go):
    require(sys.version_info >= (3,10) and sys.flags.optimize == 0, 'Python >=3.10, no -O required')
    require(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'Linux x86_64 required')
    require(cpu in os.sched_getaffinity(0), 'selected CPU not in permitted affinity mask')
    require(Path('/proc/self/stat').is_file() and Path('/proc/self/status').is_file(), 'readable procfs required')
    taskset = shutil.which('taskset')
    require(taskset is not None, 'util-linux taskset required')
    require(sha(KIT/'inputs.json') == EXPECTED_INPUTS, 'frozen input contracts differ')
    require(sha(binary) == expected_sha, 'binary digest does not match operator pin')
    with binary.open('rb') as f: h = f.read(64)
    require(len(h)==64 and h[:7]==b'\x7fELF\x02\x01\x01' and struct.unpack_from('<H',h,18)[0]==62 and struct.unpack_from('<H',h,16)[0] in (2,3), 'expected x86-64 executable ELF')
    require(os.access(binary, os.X_OK), 'binary is not executable')
    go_env={'PATH':os.defpath,'GOTOOLCHAIN':'local','GOENV':'off','GOWORK':'off','GOTELEMETRY':'off','HOME':str(Path.home())}
    def go_out(*args):
        return subprocess.check_output([str(go),*args],text=True,env=go_env,timeout=15).strip()
    host_go=go_out('version')
    build_go=go_out('version','-m',str(binary))
    require(re.search(r'\b'+re.escape(expected_go)+r'\b',host_go) is not None and 'linux/amd64' in host_go and build_go.splitlines()[0].endswith(': '+expected_go), 'Go tool or binary build version mismatch')
    st=binary.stat()
    contracts=json.loads((KIT/'inputs.json').read_text())
    artifacts={w['artifact']:sha(corpus/w['artifact']) for w in contracts}
    require(len(artifacts)==4 and all(artifacts[w['artifact']]==w['sha256'] for w in contracts), 'missing or mismatched input artifact')
    return {'machine':platform.machine(),'kernel':platform.release(),'python':platform.python_version(),
            'cpu':cpu,'permitted_affinity':sorted(os.sched_getaffinity(0)), 'taskset':taskset,
            'go':str(go),'go_tool_sha256':sha(go),'go_version':host_go,'binary_build':build_go.replace(str(binary), '<binary>',1),
            'binary':str(binary),'binary_sha256':expected_sha,'binary_dev':st.st_dev,'binary_ino':st.st_ino,
            'expected_go':expected_go,'corpus':str(corpus),'artifacts':artifacts,
            'kit':{x:sha(KIT/x) for x in PACKAGE}}
def read_checked(p, pin):
    require(sha(p)==pin, 'manifest digest mismatch')
    m=json.loads(p.read_text())
    observed=attest(Path(m['binary']),Path(m['corpus']),m['cpu'],Path(m['go']),m['binary_sha256'],m['expected_go'])
    require(m==observed, 'host/binary/input/config differs from frozen manifest')
    return m
def main():
    p=argparse.ArgumentParser(description=__doc__);sp=p.add_subparsers(dest='command',required=True)
    a=sp.add_parser('prepare')
    for n in ('binary','corpus','go','manifest'): a.add_argument('--'+n,type=Path,required=True)
    a.add_argument('--cpu',type=int,required=True)
    a.add_argument('--binary-sha256',required=True)
    a.add_argument('--expected-go',required=True)
    c=sp.add_parser('check');c.add_argument('--manifest',type=Path,required=True);c.add_argument('--manifest-sha256',required=True)
    a=p.parse_args()
    if a.command=='prepare':
        manifest=attest(a.binary.resolve(strict=True),a.corpus.resolve(strict=True),a.cpu,a.go.resolve(strict=True),a.binary_sha256,a.expected_go)
        with a.manifest.open('x') as f:
            json.dump(manifest,f,sort_keys=True,indent=2);f.write('\n');f.flush(); os.fsync(f.fileno())
        print(sha(a.manifest))
    else:
        read_checked(a.manifest,a.manifest_sha256);print('Manifest and local prerequisites verified; no adapter executed.')
if __name__=='__main__':
    main()
