import pathlib,importlib.util,sys,runpy
E=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('run',E/'run-v4.py');q=importlib.util.module_from_spec(spec);sys.modules['run']=q;spec.loader.exec_module(q)
target=pathlib.Path(sys.argv[1]);sys.argv=sys.argv[1:]
if target.name=='run.py':target=E/'run-v4.py';sys.argv[0]=str(target)
runpy.run_path(str(target),run_name='__main__')
