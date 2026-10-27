# Reconstruct exact baseline production without changing candidate files.
import json,subprocess
from pathlib import Path
root=Path(__file__).resolve().parents[3];d=Path(__file__).resolve().parent
repl={}
for name in ['compile.go','gc_direct.go','gc_struct.go']:
 p='src/core/compiler/backend/railshot/amd64/'+name
 out=d/(name+'.control.txt')
 out.write_bytes(subprocess.check_output(['git','show','cd029409bd8985ab51d30e5f68714d13d00192fa:'+p],cwd=root))
 repl[str(root/p)]=str(out)
repl[str(root/'src/core/compiler/backend/railshot/amd64/gc_cast_array_get.go')]=''
(d/'control-overlay.json').write_text(json.dumps({'Replace':repl},indent=2)+'\n')
