"""Reuse identical completed tooling commands; execute only the missing diff/format gates."""
import run as q
import pathlib,json,hashlib,gzip
s=pathlib.Path(q.M['scratch'])/'tooling';q.env['PATH']=str(s/'.tools/staticcheck')+':'+q.env['PATH']
aliases={'replay-docs':'tooling-docs','replay-actionlint':'tooling-actionlint','replay-staticcheck':'tooling-staticcheck','replay-native-compare':'tooling-native-compare'}
for n in ['build-release-assets.sh','release-qualification.sh','ci-download-go-modules.sh']:aliases['replay-'+n]='tooling-script-'+n
proof=[]
for required,receipt in aliases.items():
 d=q.E/receipt;c=json.loads((d/'command.json').read_text());r=json.loads((d/'result.json').read_text())
 assert c['source']==r['source']==q.M['pin'] and r['exit']==0 and r['driver_unchanged']
 assert c['cwd']==str(s)
 assert c['driver_sha256']==hashlib.sha256((q.E/'run.py').read_bytes()).hexdigest()
 assert r['raw_sha256']==hashlib.sha256(gzip.decompress((d/'raw.log.gz').read_bytes())).hexdigest()
 proof.append(dict(required=required,receipt=receipt,command=c['argv'],raw_sha256=r['raw_sha256'],reason='Same pinned tool/command recipe and immutable tooling checkout; historical replay name only.'))
# The first actionlint command named absolute paths in the identical source export.
for p in (q.S/'.github/workflows').glob('*.yml'):
 assert p.read_bytes()==(s/p.relative_to(q.S)).read_bytes(),p
assert (q.S/'.github/actionlint.yaml').read_bytes()==(s/'.github/actionlint.yaml').read_bytes()
(q.E/'receipt-aliases.json').write_text(json.dumps(dict(source=q.M['pin'],aliases=aliases,proof=proof),indent=2)+'\n')
q.run('replay-generated-diff',['git','diff','--exit-code','--','wago.go','schema.json'],s)
q.run('replay-format',['bash','-c','set -euo pipefail; unformatted=$(git ls-files -z -- "*.go" | xargs -0 gofmt -l); test -z "$unformatted" || { printf "%s\\n" "$unformatted"; exit 1; }'],s)
