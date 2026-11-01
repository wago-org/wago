#!/usr/bin/env python3
"""Transparent TinyGo wrapper: retain successful standalone build before TempDir cleanup."""
import os, pathlib, shutil, subprocess, sys, json, hashlib
args=sys.argv[1:]
status=subprocess.call([os.environ['REAL_TINYGO'], *args])
if status == 0 and args and args[0]=='build' and '-o' in args:
    exe=pathlib.Path(args[args.index('-o')+1])
    if exe.name=='runner':
        out=pathlib.Path(os.environ['TINYGO_CAPTURE'])
        out.mkdir(parents=True,exist_ok=True)
        target=out/('artifact-'+str(os.getpid()))
        shutil.copytree(pathlib.Path.cwd(),target)
        (target/'capture.json').write_text(json.dumps({'args':args,'cwd':str(pathlib.Path.cwd()),'tool':os.environ['REAL_TINYGO'],'sha256':hashlib.sha256(exe.read_bytes()).hexdigest()},indent=2)+'\n')
sys.exit(status)
