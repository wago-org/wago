import os,subprocess,pathlib
out=pathlib.Path(__file__).resolve().parent
env=dict(os.environ,GOMAXPROCS='1')
pattern='^BenchmarkRailshotCompile(SmallScalar|MediumControl|ALUHeavy)$'
def run(path,bench,time,name):
 with (out/name).open('w') as f:
  subprocess.run(['taskset','-c','15',path,'-test.run=^$','-test.bench='+bench,'-test.benchmem','-test.benchtime='+time,'-test.count=1'],env=env,stdout=f,stderr=subprocess.STDOUT,check=True)
(out/'host-load.txt').write_text(pathlib.Path('/proc/loadavg').read_text())
for n in range(1,6):
 for side in (['before','after'] if n%2 else ['after','before']):
  binary='/tmp/wago-815-integration-'+side+'.test'
  run(binary,pattern,'20ms',f'compile-{n}-{side}.txt')
  run(binary,'^BenchmarkBoundsFactsExecute$/^sources=8$/^guard=false$','10000x',f'exec-{n}-{side}.txt')
for n in range(1,4):
 for side in ['a1','a2']:
  run('/tmp/wago-815-integration-before.test','^BenchmarkRailshotCompileSmallScalar$','20ms',f'control-{n}-{side}.txt')
