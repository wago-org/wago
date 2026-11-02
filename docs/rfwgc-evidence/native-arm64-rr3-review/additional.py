exec(open(__file__.replace('additional.py','validate.py')).read().split("run('toolchain'")[0])
rows=json.loads((out/'commands.json').read_text())
run('actionlint-explicit-config',[go,'run','github.com/rhysd/actionlint/cmd/actionlint@v1.7.7','-config-file','.github/actionlint.yaml','.github/workflows/ci.yml'])
env.update(GOOS='linux',GOARCH='arm64',CGO_ENABLED='0')
builds=[]
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 for group,pkg in [('wago','./src/wago'),('runtime','./src/core/runtime'),('backend','./src/core/compiler/backend/railshot/arm64')]:
    binary=root.parent/(mode+'-'+group+'.test')
    label=mode+'-'+group+'-cross-build'
    run(label,[go,'test','-c',* ([] if mode=='ordinary' else ['-tags='+mode]),'-o',str(binary),pkg],source,180)
    builds.append(dict(label=label,exit=rows[-1]['exit'],elf_machine=int.from_bytes(binary.read_bytes()[18:20],'little') if binary.exists() else None,execution='NOT_RUN'))
(out/'cross-builds.json').write_text(json.dumps(builds,indent=2)+'\n')
assert all(b['exit']==0 and b['elf_machine']==183 for b in builds)
