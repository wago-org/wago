from pathlib import Path
core=Path('/tmp/native-entry-dead-zeros-core')
app=Path('/tmp/native-entry-dead-zeros-app')
lines=[]
for p,base in [(core,Path('/tmp/hour-20261011-core')),(app,Path('/tmp/hour-20261011-app'))]:
 for f in sorted(p.glob('*-0.bin')):
  name=f.name[:-6]
  b=base/('wago__libtommath__modular-arithmetic.bin' if name=='modular-arithmetic' else name+'.bin')
  data=f.read_bytes();assert data==b.read_bytes(),(f,b,len(data),len(b.read_bytes()))
  on=p/(name+'-1.bin');other=on.read_bytes()
  lines.append(f'{name}: OFF exact37174169 baseline identity {len(data)}B; ON {len(other)}B; '+('unchanged' if data==other else 'CHANGED'))
Path('/tmp/entry-dead-zeros-identity.txt').write_text('\n'.join(lines)+'\n')
print('\n'.join(lines))
