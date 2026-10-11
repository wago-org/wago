from pathlib import Path
import subprocess
root=Path('/tmp/wago-cset-lineage-76e52be8')
subprocess.run(['git','apply','--exclude=src/core/compiler/backend/railshot/arm64/policy_arm64_test.go','/tmp/integer-select-local-read-retained.patch'],cwd=root,check=True)
p=root/'src/core/compiler/backend/railshot/arm64/policy_arm64_test.go';s=p.read_text();s=s.replace('\t\t"v128-direct-results", "cold-call-local-pins", "convert-read",','\t\t"integer-select-local-read",\n\t\t"v128-direct-results", "cold-call-local-pins", "convert-read",');p.write_text(s)
for name in ['paired/main.go','paired/qos_darwin.go','paired/qos_other.go','paired/vectors.go','parity_test.go','cached_core_test.go','cached_contract_bench_test.go','cached_simple_bench_test.go']:
 p=root/'experiments/arm64-parity'/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(subprocess.check_output(['git','show','76e52be8:experiments/arm64-parity/'+name],cwd=root))
print(root,flush=True)
