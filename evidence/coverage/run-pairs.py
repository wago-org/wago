import pathlib,os,subprocess
out=pathlib.Path(__file__).resolve().parent
repo=out.parent.parent
package=repo/'tests/tools/native-compare'
env=dict(os.environ,GOMAXPROCS='1')
def run(binary,pattern,iterations,path,cwd):
 with path.open('w') as f:
  subprocess.run(['taskset','-c','15',binary,'-test.run=^$','-test.bench='+pattern,'-test.benchmem','-test.benchtime='+iterations,'-test.count=1'],env=env,cwd=cwd,stdout=f,stderr=subprocess.STDOUT,check=True)
(out/'host-load.txt').write_text(pathlib.Path('/proc/loadavg').read_text())
for n in range(1,6):
 for side in (['before','after'] if n%2 else ['after','before']):
  run('/tmp/wago815-coverage-'+side+'.test','^BenchmarkCompare','100x',out/f'portable-{n}-{side}.txt',repo)
for n in range(1,4):
 for side in (['before','after'] if n%2 else ['after','before']):
  run('/tmp/wago815-coverage-profile-'+side+'.test','^BenchmarkCompareCapturedFib$','100x',out/f'fib-compare-{n}-{side}.txt',package)
  run('/tmp/wago815-coverage-profile-'+side+'.test','^BenchmarkCaptureExistingFib$','5x',out/f'fib-capture-{n}-{side}.txt',package)
