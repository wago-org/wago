import json,sys,hashlib,subprocess
from pathlib import Path
root=Path(sys.argv[1]).resolve();out=Path(__file__).resolve().parent
original=root/'src/wago/api.go';text=original.read_text();needle='\tkeepCodeImage := false\n'
if text.count(needle)!=1:raise SystemExit('unexpected API capture seam')
copy=out/'api.go.txt';copy.write_text(text.replace(needle,'\trealTrappingGuardCapturedStats = gcCodeStats\n'+needle,1))
hook=out/'hook.go.txt';hook.write_text('//go:build linux && amd64 && wago_codegenstats\n\npackage wago\n\nvar realTrappingGuardCapturedStats *railshotModuleStats\n')
repl={str(original):str(copy),str(root/'src/wago/real_trapping_guard_capture.go'):str(hook),str(root/'src/wago/real_trapping_guard_capture_test.go'):str(out/'capture_test.go.txt')}
(out/'overlay.json').write_text(json.dumps({'Replace':repl},indent=2)+'\n')
meta={'source':str(root),'commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'api_source_sha256':hashlib.sha256(original.read_bytes()).hexdigest(),'overlay_api_sha256':hashlib.sha256(copy.read_bytes()).hexdigest(),'purpose':'diagnostic capture after ordinary public API selects exact root/helper/inline policies; no candidate source edits'}
(out/'source.json').write_text(json.dumps(meta,indent=2)+'\n');print(json.dumps(meta))
