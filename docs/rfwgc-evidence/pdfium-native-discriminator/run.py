#!/usr/bin/env python3
"""Bounded native correctness discriminator; not a performance benchmark."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

PINS = {'baseline': '9713878ab452848f7eb7549a90be94b938a898be',
        'candidate': '889b46c45fc54e2fb4b4617eb0c5cc5f54de5db7'}
TEST = 'src/wago/pdfium_corpus_test.go'
EXPECTED = [f'{w}:{phase}' for w in (1, 4) for phase in
            ('compile-begin', 'compile-end', 'execute-begin', 'execute-end')]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def overlay(text):
    assert text.count('regressiontest.RunIsolated(t, 60*time.Second)') == 1
    replacements = [
        ('\t\t\tmodule, err := rt.Compile(wasm)',
         '\t\t\tpdfiumDiagnosticPhase(t, workers, "compile-begin")\n\t\t\tmodule, err := rt.Compile(wasm)'),
        ('\t\t\tdefer module.Compiled().Close()',
         '\t\t\tpdfiumDiagnosticPhase(t, workers, "compile-end")\n\t\t\tdefer module.Compiled().Close()'),
        ('\t\t\trunPDFiumInvoice(t, rt, module, pdf, oracle)',
         '\t\t\tpdfiumDiagnosticPhase(t, workers, "execute-begin")\n\t\t\trunPDFiumInvoice(t, rt, module, pdf, oracle)\n\t\t\tpdfiumDiagnosticPhase(t, workers, "execute-end")')]
    for old, new in replacements:
        assert text.count(old) == 1, old
        text = text.replace(old, new)
    text += '''
// Diagnostic only: no timestamps, deadline or oracle changes.
func pdfiumDiagnosticPhase(t *testing.T, workers int, phase string) {
    t.Helper()
    path := os.Getenv("PDFIUM_DIAGNOSTIC_PHASE_LOG")
    if path == "" { t.Fatal("missing phase log") }
    f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
    if err != nil { t.Fatal(err) }
    _, writeErr := fmt.Fprintf(f, "%d:%s\\n", workers, phase)
    closeErr := f.Close()
    if writeErr != nil { t.Fatal(writeErr) }
    if closeErr != nil { t.Fatal(closeErr) }
}
'''
    return text


def accepted(code, raw, phases):
    return (code == 0 and '--- PASS: TestPDFiumCorpus (' in raw
            and '--- FAIL:' not in raw and '--- SKIP:' not in raw
            and 'WARNING: DATA RACE' not in raw and phases == EXPECTED)


def main():
    ap = argparse.ArgumentParser(); ap.add_argument('baseline', type=Path)
    ap.add_argument('candidate', type=Path); ap.add_argument('output', type=Path)
    ap.add_argument('--self-test', action='store_true'); a = ap.parse_args()
    if a.self_test:
        sample = (a.candidate / TEST).read_text()
        patched = overlay(sample)
        assert 'regressiontest.RunIsolated(t, 60*time.Second)' in patched
        assert accepted(0, '--- PASS: TestPDFiumCorpus (1.00s)', EXPECTED)
        for code, raw, phases in [(1, 'PASS', EXPECTED), (0, 'PASS', EXPECTED),
                (0, '--- PASS: TestPDFiumCorpus (1.00s)', EXPECTED[:-1]),
                (0, '--- PASS: TestPDFiumCorpus (1.00s)\nWARNING: DATA RACE', EXPECTED)]:
            assert not accepted(code, raw, phases)
        print('PASS overlay anchors, unchanged deadline and four rejection controls'); return 0
    if platform.system() != 'Darwin' or platform.machine() != 'arm64':
        raise SystemExit('native Darwin ARM64 required')
    out = a.output.resolve(); out.mkdir(parents=True, exist_ok=False)
    harness = Path(__file__).resolve().parents[3]
    driver = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=harness, text=True).strip()
    assert driver == os.environ['EXPECTED_HARNESS']
    assert os.environ['EXPECTED_SOURCE'] == PINS['candidate']
    env = dict(os.environ, GOTOOLCHAIN='local', GOWORK='off', GOENV='off', CGO_ENABLED='1')
    version = subprocess.check_output(['go', 'version'], env=env, text=True).strip()
    assert version == 'go version go1.27.1 darwin/arm64', version
    rows = []; source_test = None
    for label in ['baseline', 'candidate']:
        root = getattr(a, label).resolve(); cell = out / label; cell.mkdir()
        head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
        assert head == PINS[label]
        original = (root / TEST).read_bytes()
        if source_test is None: source_test = original
        assert original == source_test, 'baseline/candidate test differs'
        before = subprocess.check_output(['git', 'status', '--porcelain'], cwd=root)
        assert not before, before
        replacement = cell / 'pdfium_corpus_test.go.txt'; replacement.write_text(overlay(original.decode()))
        mapping = cell / 'overlay.json'; mapping.write_text(json.dumps({'Replace': {str(root / TEST): str(replacement)}}))
        phases = cell / 'phases.txt'; binary = cell / 'wago.test'
        build = ['go', 'test', '-c', '-race', '-tags=wago_regalloccheck', '-mod=readonly',
                 '-overlay='+str(mapping), '-o', str(binary), './src/wago']
        command = [str(binary), '-test.run=^TestPDFiumCorpus$', '-test.count=1', '-test.v', '-test.timeout=2m']
        meta = {'label':label, 'source':head, 'harness':driver, 'go_version':version,
                'original_test_sha256':sha(original), 'overlay_sha256':sha(replacement.read_bytes()),
                'build':build, 'run':command, 'cwd':str(root), 'test_cwd':str(root/'src/wago'),
                'phase_log':str(phases), 'mode':'race+wago_regalloccheck', 'child_deadline_seconds':60,
                'fixtures':{n:sha((root/'tests/corpus/regressions/pdfium'/n).read_bytes())
                            for n in ['pdfium-8111.wasm','invoice.pdf','oracle.json']}}
        (cell/'command.json').write_text(json.dumps(meta,indent=2)+'\n')
        with (cell/'build.log').open('w') as log:
            br = subprocess.run(build,cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=1200)
        result = {'label':label,'build_exit':br.returncode,'status':'BUILD_FAIL'}
        if br.returncode == 0:
            meta['binary_sha256'] = sha(binary.read_bytes())
            (cell/'command.json').write_text(json.dumps(meta,indent=2)+'\n')
            execution_env = dict(env, PDFIUM_DIAGNOSTIC_PHASE_LOG=str(phases))
            with (cell/'run.log').open('w') as log:
                try:
                    rr = subprocess.run(command,cwd=root/'src/wago',env=execution_env,
                                        stdout=log,stderr=subprocess.STDOUT,timeout=150)
                    code = rr.returncode
                except subprocess.TimeoutExpired: code = 124
            raw = (cell/'run.log').read_text(); phase_rows = phases.read_text().splitlines() if phases.exists() else []
            result.update(run_exit=code,phases=phase_rows,deadline_failure='exceeded 1m0s deadline' in raw,
                          data_race_reports=raw.count('WARNING: DATA RACE'),
                          status='PASS' if accepted(code,raw,phase_rows) else 'FAIL')
        assert (root/TEST).read_bytes() == original
        assert subprocess.check_output(['git','status','--porcelain'],cwd=root) == before
        (cell/'result.json').write_text(json.dumps(result,indent=2)+'\n'); rows.append(result)
        (out/'results.json').write_text(json.dumps(rows,indent=2)+'\n')
        if binary.exists(): binary.unlink()  # hash retained; no active process after subprocess return
    (out/'SHA256SUMS').write_text(''.join(sha(f.read_bytes())+'  '+str(f.relative_to(out))+'\n'
        for f in sorted(out.rglob('*')) if f.is_file() and f.name!='SHA256SUMS'))
    print(json.dumps(rows,indent=2))
    return 0 if all(r['status']=='PASS' for r in rows) else 1

if __name__ == '__main__': sys.exit(main())
