"""Refusal and exact census contracts; no simulated native qualification."""
import copy
import itertools
import json
import unittest
import run


class HarnessContracts(unittest.TestCase):
    def test_exact_identity(self):
        h = 'a' * 40
        run.identity(run.SOURCE, h, run.SOURCE, h)
        for args in [(h, h, h, h), (run.SOURCE, 'branch', run.SOURCE, h),
                     (run.SOURCE, h, h, h), (run.SOURCE, h, run.SOURCE, 'b' * 40)]:
            with self.assertRaises(ValueError):
                run.identity(*args)

    def test_native_refusals(self):
        env = {'RUNNER_OS': 'Linux', 'RUNNER_ARCH': 'ARM64', 'PATH': '/usr/bin:/bin'}
        run.native(('Linux', 'aarch64'), env, 183)
        for host, machine in [(('Linux', 'x86_64'), 62), (('Darwin', 'arm64'), 183),
                              (('Linux', 'aarch64'), 62)]:
            with self.assertRaises(ValueError):
                run.native(host, env, machine)
        for key, value in [('GOARCH', 'arm64'), ('GOOS', 'linux'), ('GOFLAGS', '-race'),
                           ('QEMU_CPU', 'max'), ('WAGO_BOUNDS', 'signals'),
                           ('RUNNER_ARCH', 'X64'), ('PATH', '/home/a/mise/shims')]:
            with self.assertRaises(ValueError):
                run.native(('Linux', 'aarch64'), dict(env, **{key: value}), 183)

    def test_census(self):
        events = [{'Package': './p', 'Test': 'TestA', 'Action': 'run'},
                  {'Package': './p', 'Test': 'TestA', 'Action': 'pass'},
                  {'Package': './p', 'Action': 'pass'}]
        def check(xs):
            return run.census('\n'.join(json.dumps(x) for x in xs), ['TestA'], './p')['ok']
        self.assertTrue(check(events))
        self.assertFalse(check(events[1:]))
        self.assertFalse(check(events[:-1]))
        self.assertFalse(check(events + events[:2]))
        self.assertFalse(check(events + [{'Package': './p', 'Test': 'TestA/sub', 'Action': 'skip'}]))
        for action in ['skip', 'fail']:
            bad = copy.deepcopy(events)
            bad[1]['Action'] = action
            self.assertFalse(check(bad))
        bad = copy.deepcopy(events)
        bad[1]['Test'] = 'TestOther'
        self.assertFalse(check(bad))
        bad = copy.deepcopy(events)
        bad[1]['Package'] = './other'
        self.assertFalse(check(bad))
        with self.assertRaises(ValueError):
            run.census('not json', ['TestA'], './p')

    def test_summary_states(self):
        # The native summary accepts only successful matrix + skipped other jobs.
        accepted = []
        for native, normal, darwin in itertools.product(['success', 'failure', 'cancelled', 'skipped'], repeat=3):
            if native == 'success' and all(x == 'skipped' for x in [normal, darwin]):
                accepted.append((native, normal, darwin))
        self.assertEqual(accepted, [('success', 'skipped', 'skipped')])

    def test_required_originals(self):
        suites = run.PLAN['suites']
        self.assertEqual([len(suites[k]['tests']) for k in ['focused', 'guard', 'runtime', 'backend']], [85, 1, 11, 9])
        focused = suites['focused']['tests']
        for test in ['TestRRRecheckStorageOnlyReturnBehindDynamicReader',
                     'TestRRRecheckCanceledAdmissionBehindDynamicReader',
                     'TestRRRecheckSharedMemoryHandoff', 'TestRRRetirementBorrowedRootLatePublisher',
                     'TestRRRetirementCanceledPartialLastPins', 'TestRRRetirementCallbackRefreshBehindReader']:
            self.assertIn(test, focused)
        self.assertEqual(suites['guard']['tests'], ['TestSharedEntryGuardCancellationAndGC'])


if __name__ == '__main__':
    unittest.main()
