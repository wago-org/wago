"""Losslessly retain plain logs with an uncompressed-content manifest."""
import pathlib,gzip,json,hashlib
E=pathlib.Path(__file__).resolve().parent;rows=[]
for p in sorted(E.rglob('*.log')):
 data=p.read_bytes();dest=pathlib.Path(str(p)+'.gz');assert not dest.exists(),str(dest)
 dest.write_bytes(gzip.compress(data,mtime=0));assert gzip.decompress(dest.read_bytes())==data
 rows.append(dict(original=str(p.relative_to(E)),compressed=str(dest.relative_to(E)),raw_sha256=hashlib.sha256(data).hexdigest(),bytes=len(data)));p.unlink()
(E/'compressed-log-manifest.json').write_text(json.dumps(rows,indent=2)+'\n');print('losslessly compressed logs',len(rows))
