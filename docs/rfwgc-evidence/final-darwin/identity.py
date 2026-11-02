#!/usr/bin/env python3
"""Validate dispatch identities before fetching or executing the source."""
import json
import os
from pathlib import Path
import re
import subprocess

HERE = Path(__file__).resolve().parent
SOURCE = json.loads((HERE / 'plan.json').read_text())['source']


def validate(source, driver):
    if not re.fullmatch(r'[0-9a-f]{40}', source) or source != SOURCE:
        raise ValueError('source_sha must equal the full reviewed plan source: ' + SOURCE)
    if not re.fullmatch(r'[0-9a-f]{40}', driver):
        raise ValueError('QUALIFICATION_DRIVER must be a full commit SHA')
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=HERE, text=True).strip()
    if head != driver:
        raise ValueError('harness HEAD differs from workflow execution SHA')
    dirt = subprocess.check_output(['git', 'status', '--porcelain', '--untracked-files=all'], cwd=HERE, text=True)
    if dirt:
        raise ValueError('harness tracked tree is dirty')
    return {'source': source, 'driver': driver}


if __name__ == '__main__':
    print(json.dumps(validate(os.environ.get('QUALIFICATION_SOURCE', ''),
                              os.environ.get('QUALIFICATION_DRIVER', ''))))
