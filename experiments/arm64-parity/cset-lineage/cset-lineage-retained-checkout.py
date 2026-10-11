import subprocess,os
from pathlib import Path
repo=Path('/Users/work/Code/Wago/wago');dst=Path('/tmp/wago-cset-lineage-retained-7257c1c0')
subprocess.run(['git','worktree','add','--detach','--no-checkout',str(dst),'7257c1c0'],cwd=repo,check=True)
subprocess.run(['git','sparse-checkout','init','--cone'],cwd=dst,check=True)
subprocess.run(['git','sparse-checkout','set','src','internal','codegen','cli','profile','tests','corpus/workloads/assemblyscript','corpus/workloads/semantic/coremark','corpus/workloads/synthetic'],cwd=dst,check=True)
subprocess.run(['git','checkout'],cwd=dst,check=True)
for name in ['paired/main.go','paired/qos_darwin.go','paired/qos_other.go','paired/vectors.go','parity_test.go','cached_core_test.go','cached_contract_bench_test.go','cached_simple_bench_test.go']:
 rel='experiments/arm64-parity/'+name;p=dst/rel;p.parent.mkdir(parents=True,exist_ok=True)
 p.write_bytes(subprocess.check_output(['git','show','7257c1c0:'+rel],cwd=repo))
subprocess.run(['git','apply','/tmp/cset-lineage-retained.patch'],cwd=dst,check=True)
print('Retained source tree ready:',dst,flush=True)
