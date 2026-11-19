#!/usr/bin/env python3
# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.
#
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) 2026 Wago authors and contributors

# make-twins.py CORPUS_CACHE OUTDIR: build _start twins of wasm.fyi workloads.
# Each twin is the wasm.fyi artifact unchanged plus a _start that calls the
# workload export with wasm.fyi's arguments and traps unless the result matches
# wasm.fyi's oracle. CORPUS_CACHE is a wasm.fyi .wasmbench/corpus-cache
# directory (manifest.json + artifacts/). Requires wasm-tools.
import json,subprocess,sys,re,os
C=sys.argv[1]
m={w['id']:w for w in json.load(open(C+'/manifest.json'))['workloads']}
PICK=[('pcre2','wago/pcre2/compile-match'),('yyjson','wago/yyjson/parse-edit-write'),('utf8proc','wago/utf8proc/normalize-casefold'),
      ('nanosvg','wago/nanosvg/parse-structure'),('libtommath','wago/libtommath/modular-arithmetic'),('coremark','wago/coremark/2k-performance')]
out=sys.argv[2]; meta=[]
for name,i in PICK:
  w=m[i]; wat=subprocess.check_output(['wasm-tools','print',C+'/'+w['artifact']],text=True)
  # find the exported func's index/name and signature
  ex=re.search(r'\(export "%s" \(func (\S+)\)\)'%re.escape(w['export']),wat).group(1)
  fn=re.search(r'\(func %s \(;\d+;\) \(type (\d+)\)'%re.escape(ex) if ex.startswith('$') else r'\(func \(;%s;\) \(type (\d+)\)'%ex,wat) or re.search(r'\(func %s (?:\(;\d+;\) )?\(type (\d+)\)'%re.escape(ex),wat)
  ty=re.search(r'\(type \(;%s;\) \(func([^\n]*)\)\)'%fn.group(1),wat).group(1)
  params=re.findall(r'\(param ([^)]*)\)',ty); params=' '.join(params).split()
  results=' '.join(re.findall(r'\(result ([^)]*)\)',ty)).split()
  assert len(params)==len(w['args']),(i,params,w['args'])
  assert 'start' not in [ln.strip()[:6] for ln in wat.splitlines()] and '"_start"' not in wat
  body=''.join(f'{t}.const {a} ' for t,a in zip(params,w['args']))+f'call {ex} '
  exp=w['oracle']['expected']
  for k in reversed(range(len(results))):
    t=results[k]; body+=f'{t}.const {exp[k]} {t}.ne if unreachable end '
  wat=wat.rstrip()
  assert wat.endswith(')')
  wat=wat[:-1]+f'  (func $__wfyi_start {body})\n  (export "_start" (func $__wfyi_start))\n)\n'
  dst=f'{out}/{name}.wasm'
  subprocess.run(['wasm-tools','parse','-o',dst,'-'],input=wat,text=True,check=True)
  subprocess.run(['wasm-tools','validate',dst],check=True)
  meta.append({'name':name,'id':i,'export':w['export'],'args':w['args'],'expected':exp,'sha256':w['sha256'],'params':params,'results':results,'desc':w['original_contract'].get('desc','')})
json.dump(meta,open(f'{out}/wasmfyi.json','w'),indent=1)
for x in meta: print(x['name'],x['id'],x['params'],'->',x['results'],x['args'],x['expected'],'|',x['desc'][:70])
