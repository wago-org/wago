#!/usr/bin/env python3
"""Check that retained exports still match pinned source and driver bytes."""
import hashlib,io,json,pathlib,subprocess,tarfile
root=pathlib.Path(__file__).resolve().parent
reports=[]
for directory,driver in [('qualified','run.py'),('guest-replay','run-guests.py'),('rv64-guest-replay','run-rv64-guests.py')]:
 meta=json.loads((root/directory/'source.json').read_text());src=pathlib.Path(meta['export'])
 raw=subprocess.check_output(['git','archive',meta['pin'],'src'])
 matches=0;changed=[]
 with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
  for member in archive:
   if not member.isfile():continue
   expected=archive.extractfile(member).read();actual=(src/member.name).read_bytes()
   if actual!=expected:changed.append(member.name)
   matches+=1
 h=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
 tools={name:h(src.parent/'qemu/usr/bin'/name) for name in ['qemu-aarch64','qemu-riscv64']}
 report={'directory':directory,'pin':meta['pin'],'compared_files':matches,'changed_tracked_source':changed,'driver_matches':h(root/driver)==meta['driver_sha256'],'emulator_sha256':tools}
 reports.append(report)
 assert not changed and report['driver_matches'],report
(root/'source-verification.json').write_text(json.dumps(reports,indent=2)+'\n')
