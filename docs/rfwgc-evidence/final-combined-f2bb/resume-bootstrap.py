import importlib.util, pathlib, sys, runpy
E=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('run',E/'resume-run.py')
q=importlib.util.module_from_spec(spec);sys.modules['run']=q;spec.loader.exec_module(q)
script=pathlib.Path(sys.argv[1]);sys.argv=sys.argv[1:]
if script.name=='run.py':script=E/'resume-run.py'
runpy.run_path(str(script),run_name='__main__')
