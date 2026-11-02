#!/usr/bin/env python3
import run as q
import pathlib
q.env.pop('GOWORK',None)
s=pathlib.Path(q.M['scratch'])/'tooling'
q.env['PATH']=str(s/'.tools/staticcheck')+':'+q.env['PATH']
for name,cmd in [('docs',[q.G+'/bin/go','run','./tests/tools/docs-check']),('generated-diff',['git','diff','--exit-code','--','wago.go','schema.json']),('format',['bash','-c','set -euo pipefail; unformatted=$(git ls-files -z -- "*.go" | xargs -0 gofmt -l); test -z "$unformatted" || { printf "%s\\n" "$unformatted"; exit 1; }'])]:q.run('replay-'+name,cmd,s)
for name in ['build-release-assets.sh','release-qualification.sh','ci-download-go-modules.sh']:q.run('replay-'+name,['bash','tests/scripts/'+name],s)
q.run('replay-actionlint',[q.G+'/bin/go','run','github.com/rhysd/actionlint/cmd/actionlint@v1.7.7',*[str(p.relative_to(s)) for p in sorted((s/'.github/workflows').glob('*.yml'))]],s)
q.run('replay-staticcheck',[q.G+'/bin/go','run','./tests/tools/staticcheck-baseline','verify'],s)
