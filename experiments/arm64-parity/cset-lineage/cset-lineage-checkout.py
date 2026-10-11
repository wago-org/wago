from pathlib import Path
import subprocess
repo='/Users/work/Code/Wago/wago';dst='/tmp/wago-cset-lineage-76e52be8'
subprocess.run(['git','worktree','add','--detach','--no-checkout',dst,'76e52be8'],cwd=repo,check=True)
subprocess.run(['git','sparse-checkout','init','--cone'],cwd=dst,check=True)
subprocess.run(['git','sparse-checkout','set','src','internal','codegen','cli','profile','tests','corpus/workloads/assemblyscript','corpus/workloads/semantic/coremark','corpus/workloads/synthetic'],cwd=dst,check=True)
subprocess.run(['git','checkout','--detach','76e52be8'],cwd=dst,check=True)
subprocess.run(['git','apply','/tmp/integer-select-local-read-retained.patch'],cwd=dst,check=True)
for name in ['paired/main.go','paired/qos_darwin.go','paired/qos_other.go','paired/vectors.go','parity_test.go','cached_core_test.go','cached_contract_bench_test.go','cached_simple_bench_test.go']:
 p=Path(dst)/'experiments/arm64-parity'/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(subprocess.check_output(['git','show','76e52be8:experiments/arm64-parity/'+name],cwd=dst))
print(dst,flush=True)
