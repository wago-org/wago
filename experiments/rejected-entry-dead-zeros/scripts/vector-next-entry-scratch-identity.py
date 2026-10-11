from pathlib import Path
import sys
version=sys.argv[1] if len(sys.argv)>1 else 'scratch'
proof=[]
for mode in ['signals','explicit']:
 for kind,count in [('app',102),('core',46)]:
  source=Path(f'/tmp/entry-dead-zeros-qual-{mode}-{kind}')
  new=Path(f'/tmp/entry-dead-zeros-{version}-{mode}-on/{kind}')
  fs=list(source.glob('*.bin')); assert len(fs)==count,(source,len(fs))
  assert len(list(new.glob('*.bin')))==count,new
  for f in fs: assert f.read_bytes()==(new/f.name).read_bytes(),(mode,kind,f.name)
  proof.append(f'{mode} ON {kind}: all {count} native images identical to the previously exact-qualified ON source.')
for kind,count in [('app',102),('core',46)]:
 source=Path(f'/tmp/hour-20261011-{kind}')
 new=Path(f'/tmp/entry-dead-zeros-{version}-signals-off/{kind}')
 fs=list(source.glob('*.bin'));assert len(fs)==count,(source,len(fs))
 assert len(list(new.glob('*.bin')))==count,new
 for f in fs: assert f.read_bytes()==(new/f.name).read_bytes(),('OFF',kind,f.name)
 proof.append(f'signals OFF {kind}: all {count} native images identical to exact37174169.')
out=Path(f'/tmp/wago-vector-next-37174169/experiments/arm64-parity/entry-dead-zeros/{version}-native-proof.txt')
out.write_text('\n'.join(proof)+'\n')
print(out.read_text())
