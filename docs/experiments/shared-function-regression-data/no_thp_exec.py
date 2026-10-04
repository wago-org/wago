# Diagnostic only: a per-process policy inherited across exec; no global change.
import ctypes,os,sys
libc=ctypes.CDLL(None,use_errno=True)
libc.prctl.argtypes=[ctypes.c_int,ctypes.c_ulong,ctypes.c_ulong,ctypes.c_ulong,ctypes.c_ulong]
libc.prctl.restype=ctypes.c_int
if libc.prctl(41,1,0,0,0)!=0:raise OSError(ctypes.get_errno(),'PR_SET_THP_DISABLE')
os.execvp(sys.argv[1],sys.argv[1:])
