import os,sys,time,json,subprocess,pathlib
label,*cmd=sys.argv[1:];root=pathlib.Path('/tmp/wago-pr948-main-sync-20261022-r2-evidence');start=time.monotonic()
with (root/(label+'.log')).open('w') as log:
 p=subprocess.Popen(cmd,stdout=log,stderr=subprocess.STDOUT);pid,status,usage=os.wait4(p.pid,0);p.returncode=os.waitstatus_to_exitcode(status)
r=dict(command=cmd,seconds=time.monotonic()-start,exit=p.returncode,user_seconds=usage.ru_utime,system_seconds=usage.ru_stime,peak_rss_kib=usage.ru_maxrss)
(root/(label+'.json')).write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r));print((root/(label+'.log')).read_text()[-5000:]);sys.exit(p.returncode)
