import sys,subprocess,resource,json
r=subprocess.run(sys.argv[2:]);u=resource.getrusage(resource.RUSAGE_CHILDREN)
with open(sys.argv[1],'w')as f:json.dump({'exit':r.returncode,'peak_rss_kib':u.ru_maxrss,'user_seconds':u.ru_utime,'system_seconds':u.ru_stime,'minor_faults':u.ru_minflt,'major_faults':u.ru_majflt,'voluntary_switches':u.ru_nvcsw,'involuntary_switches':u.ru_nivcsw},f,indent=2)
sys.exit(r.returncode)
