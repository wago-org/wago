"""Extra native gates. Child exit zero is insufficient; no source overlay."""
import json
import re

FIXTURE = 'gc/const-expr-gc.wast'
NONCE = '00112233445566778899aabbccddeeff'
MARKER = 'WAGO_REGRESSION_FIXTURE_OUTCOME='
CANCELLATION = 'TestSharedEntryGuardCancellationAndGC'
RESERVATION = ['TestGCARM64LiveStructDoesNotPrepareUnusedHandles',
               'TestGCARM64WideArrayDoesNotPrepareUnconsumableState']
TWO_RESULT = ['TestTwoIntegerResultRegisterCall']
REPAIRED = [
    'TestExternrefGlobalHoldingConvertedGCReference',
    'TestGCExternConversionAcrossReferenceTables',
    'TestFixedSharedDirectGoImport', 'TestFixedSharedDirectGoCallerScope',
    'TestFixedSharedDirectGoImportPolicy', 'TestSharedSingleEntryGuardAndGoFallback',
    'TestSharedEntryRegisterResultAndGoFallback', 'TestSharedEntryRegisterArgumentAndGoFallback',
    'TestSharedEntryGuardForeignFallback', 'TestSharedSingleNativePinsAndGoFallback',
]


def lanes(mode):
    flags = [] if mode == 'ordinary' else ['-tags='+mode]
    result = []
    for name, tests, env in [
        ('cancellation', [CANCELLATION], {}),
        ('core3-child', ['TestWasmtimeCore3Corpus'], {
            'WAGO_WASMTIME_CORE3_FIXTURE': FIXTURE,
            'WAGO_REGRESSION_CHILD_PROTOCOL': '2',
            'WAGO_REGRESSION_CHILD_NONCE': NONCE, 'WAGO_BOUNDS': 'explicit'}),
        ('repaired-contracts', REPAIRED, {}),
    ]:
        result.append({'id': name+'-'+mode, 'mode': mode, 'cwd': '.',
            'argv': ['go', 'test', '-json', '-v', '-count=1', '-timeout=30s', *flags,
                     '-run=^('+'|'.join(tests)+')$', './src/wago'],
            'environment': env, 'expected_tests': tests, 'discriminator': name})
    # Keep these independent from full race/diagnostics, which remain required.
    for name, tests, package, extra, environment in [
        ('reservation', RESERVATION, './src/wago', ['-race', *flags], {}),
        ('two-result', TWO_RESULT, './src/core/compiler/backend/railshot/arm64',
         ['-tags=wago_codegenstats'+('' if mode == 'ordinary' else ','+mode)],
         {'WAGO_SHARED_SCALAR': '0'}),
    ]:
        result.append({'id': name+'-'+mode, 'mode': mode, 'cwd': '.',
            'argv': ['go', 'test', '-json', '-v', '-count=1', '-timeout=30s', *extra,
                     '-run=^('+'|'.join(tests)+')$', package],
            'environment': environment, 'expected_tests': tests, 'discriminator': name})
    return result


def evaluate(lane, events, commands):
    passed = [e['Test'] for e in events if e.get('Action') == 'pass' and e.get('Test') and '/' not in e['Test']]
    ok = sorted(passed) == sorted(lane['expected_tests']) and not any(e.get('Action') in ('fail', 'skip') for e in events)
    result = {'exact_test_census': ok, 'expected_tests': lane['expected_tests'],
              'passing_top_level_tests': passed,
              'terminal_events': [e for e in events if e.get('Action') in ('pass', 'fail', 'skip')]}
    if lane['discriminator'] != 'core3-child':
        return result, ok
    output = ''.join(e.get('Output', '') for e in events)
    payloads = []
    malformed = False
    for line in output.splitlines():
        if MARKER in line:
            try:
                payloads.append(json.loads(line.split(MARKER, 1)[1]))
            except ValueError:
                malformed = True
    expected = dict(protocol='2', fixture=FIXTURE, nonce=NONCE,
                    modules_passed=10, modules_failed=0, modules_skipped=0,
                    assertions_passed=18, assertions_failed=0, assertions_skipped=0)
    exact = not malformed and payloads == [expected]
    reasons = []
    for line in output.splitlines():
        match = re.search(r'gc/const-expr-gc(?:\.wast)+:(\d+) module (compile|imports|instantiate) rejected: (.*)', line)
        if match:
            site = int(match[1])
            definition = None
            mapped = []
            for command in commands['commands']:
                if command['type'] == 'module_definition':
                    definition = command.get('filename')
                if command.get('line') == site:
                    mapped.append({'command': command, 'definition': definition})
            reasons.append({'line': site, 'phase': match[2], 'reason': match[3], 'commands': mapped, 'raw': line})
    result.update(child_outcomes=payloads, malformed_marker=malformed, exact_child_census=exact,
                  rejection_sites=reasons, raw_child_output=output,
                  reason_ceiling='Only emitted rejection lines are observed reasons; commands.json preserves all assertion sites. Missing assertion reasons are not inferred.')
    return result, ok and exact
