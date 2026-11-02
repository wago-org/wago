import datetime,hashlib,json,pathlib,zipfile
P=pathlib.Path(__file__).resolve().parent
source='21d7be58952b64435110246f65d44e11046c5083'; harness='6d52c087f3136f0cb4d5457c733e1a92449df08b'
run=json.loads((P/'run.json').read_text());assert run['head_sha']==harness and run['event']=='workflow_dispatch'
jobs=json.loads((P/'jobs.json').read_text())['jobs'];darwin=[]
for j in jobs:
 if not j['name'].startswith('Pinned Darwin correctness'):continue
 assert j['conclusion']=='cancelled' and j['runner_id']==0 and not j['runner_name'] and not j['steps']
 annotations=json.loads((P/f"annotations-{j['id']}.json").read_text());messages=[a['message'] for a in annotations]
 assert 'The job was not acquired by Runner of type hosted even after multiple attempts' in messages
 assert any('capacity constraints' in m and 'macOS arm64' in m for m in messages)
 elapsed=(datetime.datetime.fromisoformat(j['completed_at'].replace('Z','+00:00'))-datetime.datetime.fromisoformat(j['started_at'].replace('Z','+00:00'))).total_seconds();assert elapsed==901
 darwin.append({'name':j['name'],'job_id':j['id'],'verdict':'NOT_RUN_RUNNER_ACQUISITION_CANCELLED','recorded_interval_seconds':elapsed,'executed_steps':0,'runner_id':0,'messages':messages})
assert len(darwin)==3
expected={'TestDesignRepairPublicTableOrdinary','TestDesignRepairUnsupportedBulkAtomicity','TestDesignRepairUnsupportedConvertedPublicToken','TestUnsupportedConversionPublicGlobalRejectsOpaqueBeforeDirectory','TestUnsupportedConversionGlobalValidationOrdering','TestQualificationWindowsGCAdmission'}
windows=[];arts=json.loads((P/'artifacts.json').read_text())['artifacts'];assert len(arts)==3
for a in arts:
 path=P/(a['name']+'.zip');assert path.stat().st_size==a['size_in_bytes'];assert 'sha256:'+hashlib.sha256(path.read_bytes()).hexdigest()==a['digest']
 with zipfile.ZipFile(path) as z:
  assert z.testzip() is None
  summary=json.loads(z.read('summary.json'));assert summary['source']==source and summary['driver']==harness and summary['status']=='PASS' and summary['exit_codes']==[0,0]
  events=[json.loads(line) for line in z.read('tests.jsonl').splitlines() if line.strip()]
  assert not [e for e in events if e.get('Action') in ('fail','skip')]
  passes=[e['Test'] for e in events if e.get('Action')=='pass' and e.get('Test') and '/' not in e['Test']]
  assert len(passes)==6 and set(passes)==expected
  windows.append({'artifact':a['name'],'top_level_passes':passes,'failures':0,'skips':0,'source':source,'harness':harness,'scope':'storage/admission only, not Windows GC JIT'})
with zipfile.ZipFile(P/'full-job-logs.zip') as z:
 assert z.testzip() is None
 assert not any('Pinned Darwin' in n for n in z.namelist())
result={'status':'AUDIT_VERIFIED_DARWIN_NOT_RUN','darwin':darwin,'windows':windows,'source':source,'harness':harness,'code_repair_indicated_by_attempt':False,'queue_and_revalidation_estimate':'UNKNOWN','runtime_performance_measured':False}
(P/'audit.json').write_text(json.dumps(result,indent=2)+'\n');print('Verified: three unassigned Darwin cancellations; three Windows six-test passes; all artifact digests match.')
