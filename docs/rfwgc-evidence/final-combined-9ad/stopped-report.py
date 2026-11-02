"""Apply parent-confirmed RR3 to candidate status without rewriting gate outcomes."""
import pathlib,json
E=pathlib.Path(__file__).resolve().parent
p=E/'final-summary.json';s=json.loads(p.read_text());s.update(verdict='FAILED / INCOMPLETE',blocking_finding=json.loads((E/'blocking-finding.json').read_text()),execution_state='STOPPED; await repaired pin; no more 9ad qualification authorized')
p.write_text(json.dumps(s,indent=2)+'\n')
remaining=[r['suite'] for r in s['required'] if r['verdict']!='PASS'];done=[r['suite'] for r in s['required'] if r['verdict']=='PASS']
(E/'remaining-gates.json').write_text(json.dumps({'source':s['source'],'remaining_count':len(remaining),'remaining':remaining,'completed':done,'candidate_verdict':s['verdict'],'resume_authorized':False},indent=2)+'\n')
print(s['verdict'],s['counts'])
