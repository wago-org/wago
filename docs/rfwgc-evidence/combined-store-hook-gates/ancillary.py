#!/usr/bin/env python3
import run as q
import sys
q.env.pop('GOWORK',None)
for mode in ['ordinary','wagodebug','wago_regalloccheck']:
 suffix='' if mode=='ordinary' else ','+mode
 if q.run('runtime-cli-build-'+mode,[q.G+'/bin/go','build','-tags=wago_runtime'+suffix,'./cli/...']):sys.exit(1)
 if q.test('profile-metadata-'+mode,['./internal/jitprofile','./profile','./internal/profcapture','./internal/profilecmd','./cli/internal/profiling'],'wago_profile'+suffix,['-failfast']):sys.exit(1)
 if q.test('profile-runtime-'+mode,['./src/wago','./src/core/compiler/backend/railshot/...'],'wago_profile'+suffix,['-failfast','-run','^Test(CodeProfile|Profile|AdapterUnwind)']):sys.exit(1)
 if q.test('shared-scalar-'+mode,['./src/core/compiler/backend/railshot/...','./src/wago'],'wago_codegenstats'+suffix,['-failfast','-run','^Test(SharedScalar|ScalarIncoming|ScalarRawIncoming|ScalarValueBounds)'],extra={'WAGO_SHARED_SCALAR':'1'}):sys.exit(1)
 if q.test('native-join-'+mode,['./src/core/compiler/backend/railshot/amd64'],mode,['-failfast','-run','^Test(IntegerLiveAcrossJoin|JoinPressure)'],extra={'WAGO_SHARED_SCALAR':'0','WAGO_JOIN_NATIVE_REQUIRED':'1'}):sys.exit(1)
 if q.test('seeded-concurrency-'+mode,['./tests/integration/runtimeconcurrency'],mode,['-failfast','-run','^TestRuntimeConcurrency'],extra={'WAGO_CONCURRENCY_SEED':'439000001,439000019,439000043,439000081','WAGO_CONCURRENCY_ROUNDS':'24'}):sys.exit(1)
