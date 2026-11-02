"""Run existing local validator while redirecting only its receipt to owned scope.
Run with PYTHONDONTWRITEBYTECODE=1, explicit Go 1.27.1 PATH/GOROOT,
GOTOOLCHAIN=local. Does not execute native or broad runtime lanes.
"""
import sys
from pathlib import Path
from unittest.mock import patch
OUT=Path(__file__).resolve().parents[1]
HERE=OUT.parent/'final-darwin'
sys.path.insert(0,str(HERE))
import validate_local, contracts
original=Path.write_text
def redirect(path,data,*args,**kwargs):
    if path == contracts.REV/'local-validation.json':
        path=OUT/'local-validation.json'
    return original(path,data,*args,**kwargs)
with patch.object(Path,'write_text',redirect):
    validate_local.main()
