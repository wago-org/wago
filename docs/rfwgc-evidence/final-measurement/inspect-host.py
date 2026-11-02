#!/usr/bin/env python3
"""One-shot read-only host/namespace/lock evidence; never takes a flock."""
import os,pathlib,json,subprocess,hashlib
H=pathlib.Path(__file__).resolve().parent

def read(p):
 try:return pathlib.Path(p).read_text().strip()
 except OSError as e:return {'unreadable':str(e)}
def command(argv):
 p=subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True);return {'argv':argv,'exit':p.returncode,'output':p.stdout}
paths=['/proc/locks','/proc/self/status','/proc/self/cgroup','/proc/1/cgroup','/proc/cpuinfo','/sys/devices/system/cpu/online','/sys/devices/system/cpu/cpufreq/boost','/sys/firmware/acpi/platform_profile','/sys/fs/cgroup/cpu.max','/sys/fs/cgroup/cpuset.cpus.effective']
for pattern in ['/sys/devices/system/cpu/cpu*/topology/*','/sys/devices/system/cpu/cpufreq/policy*/*','/sys/class/hwmon/hwmon*/name','/sys/class/hwmon/hwmon*/temp*_*','/sys/class/power_supply/*/online','/sys/class/power_supply/*/status','/sys/class/powercap/*/constraint*','/sys/class/thermal/thermal_zone*/type','/sys/class/thermal/thermal_zone*/temp']:
 import glob
 paths+=glob.glob(pattern)
files={p:read(p) for p in paths if not pathlib.Path(p).is_dir()}
locks={}
for p in ['/home/dev/Projects/wago/.performance-measurement.lock','/tmp/gc-trim-regate-laptop-timing.lock','/tmp/gc-wakejoin-laptop-measurement.lease','/tmp/gc-trim-regate-laptop-measurement.lease']:
 try:
  s=os.stat(p); locks[p]={'device':s.st_dev,'major':os.major(s.st_dev),'minor':os.minor(s.st_dev),'inode':s.st_ino,'record':read(p)}
 except OSError as e:locks[p]={'error':str(e)}
ns={}
for pid in ['self','1']:
 for n in ['pid','mnt','user','cgroup']:
  p=f'/proc/{pid}/ns/{n}'
  try:ns[p]=os.readlink(p)
  except OSError as e:ns[p]=str(e)
out={'scope':'read-only snapshot; no flock attempt; container PID/mount namespace cannot establish global drain or host lock absence','affinity':sorted(os.sched_getaffinity(0)),'namespaces':ns,'files':files,'locks':locks,'commands':[command(c) for c in [['uname','-a'],['lscpu','-J'],['lscpu','-e=CPU,CORE,SOCKET,NODE,ONLINE,MAXMHZ'],['ps','-eo','pid,ppid,comm,args','--sort=pid'],['lsns']]],'proposed_serial_cpu':0,'proposed_parallel_cpus':[0,2,4,6,8,10,12,14],'owner_attestations_required':['Host PID/mount namespaces and remote runners, not merely this container','CPU reservation includes SMT siblings 1,3,5,7,9,11,13,15','Claude review and local Darwin CI preparation drain','Slipstream build/test and all other project workers drain','Canonical and legacy inode holder/grant reconciliation across namespaces','Power policy stability, CPU package sensors and throttling visibility']}
(H/'host-readonly.json').write_text(json.dumps(out,indent=2)+'\n')
