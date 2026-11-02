#!/usr/bin/env python3
import run as q
import sys
q.env.pop('GOWORK',None)
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 tags='wago_guardpage'+('' if mode=='ordinary' else ','+mode)
 if q.test('spec-v3-signals-'+mode,['./src/wago'],tags,['-failfast','-run','^TestSpecSuiteExec$'],extra={'WAGO_BOUNDS':'signals','WAGO_SPEC_VERSION':'3.0','WAGO_SPECTEST_DIR':str(q.S/'tests/conformance/spec-v3'),'WAGO_SPEC_REQUIRE_ZERO_GAPS':'1'}):sys.exit(1)
 if q.test('guard-corpus-'+mode,['./suite','-args','-wago.corpus=all'],tags,['-failfast','-run','^(TestCorpus|TestJsonAsGuardCorrect)$'],q.S/'bench'):sys.exit(1)
