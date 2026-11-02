from pathlib import Path
import difflib,json,subprocess,re
R=Path(__file__).resolve().parent; repo=R.parents[2];E=R.parent/'darwin-tinygo-supplement'
base=(repo/'.github/workflows/ci.yml').read_text();s=base
s=s.replace('    inputs:\n','    inputs:\n      darwin_tinygo_supplement:\n        description: Run only the reviewed exact-pin Darwin TinyGo supplement\n        type: boolean\n        default: false\n',1)
s=s.replace('(!inputs.darwin_qualification && !inputs.native_arm64_rr3)','(!inputs.darwin_qualification && !inputs.native_arm64_rr3 && !inputs.darwin_tinygo_supplement)')
s=s.replace('(inputs.darwin_qualification || inputs.native_arm64_rr3)','(inputs.darwin_qualification || inputs.native_arm64_rr3 || inputs.darwin_tinygo_supplement)')
lines=s.splitlines()
for i,line in enumerate(lines):
 if 'if:' in line and "github.event_name == 'workflow_dispatch'" in line and ('inputs.native_arm64_rr3' in line or 'inputs.darwin_qualification' in line):
  lines[i]=line+' && !inputs.darwin_tinygo_supplement'
 if line.startswith('  group: ci-'):
  lines[i]=line+"${{ github.event_name == 'workflow_dispatch' && inputs.darwin_tinygo_supplement && format('-darwin-tinygo-{0}', github.run_id) || '' }}"
 if line=='  cancel-in-progress: true':lines[i]="  cancel-in-progress: ${{ !(github.event_name == 'workflow_dispatch' && inputs.darwin_tinygo_supplement) }}"
s='\n'.join(lines)+'\n'
proposal=(E/'workflow.proposal.yml').read_text().split('jobs:\n',1)[1]
native=proposal.split('  summary:\n',1)[0].replace('  native:\n','  darwin-tinygo-native:\n',1)
native=native.replace('    runs-on: macos-15',"    if: github.event_name == 'workflow_dispatch' && inputs.darwin_tinygo_supplement\n    runs-on: macos-15",1)
native=native.replace('      MODE: ${{ matrix.mode }}','      OTHER_DARWIN: ${{ inputs.darwin_qualification }}\n      OTHER_ARM: ${{ inputs.native_arm64_rr3 }}\n      MODE: ${{ matrix.mode }}',1)
native=native.replace('          set -euo pipefail\n','          set -euo pipefail\n          [[ "$OTHER_DARWIN" == false && "$OTHER_ARM" == false ]]\n',1)
oldjobs=re.findall(r'^  ([a-zA-Z0-9_-]+):$',base.split('jobs:\n',1)[1],re.M);needs=oldjobs+['darwin-tinygo-native']
summary='''  darwin-tinygo-summary:
    name: Darwin TinyGo supplement complete
    if: always() && github.event_name == 'workflow_dispatch' && inputs.darwin_tinygo_supplement
    needs: NEEDS
    runs-on: ubuntu-latest
    timeout-minutes: 3
    env:
      RESULTS: ${{ toJSON(needs) }}
    steps:
      - shell: bash
        run: |
          python3 -c 'import json,os; n=json.loads(os.environ["RESULTS"]); assert n["darwin-tinygo-native"]["result"] == "success", n; assert all(v["result"] == "skipped" for k,v in n.items() if k != "darwin-tinygo-native"), n'
'''.replace('NEEDS',json.dumps(needs))
s+= '\n'+native+summary
(R/'ci.integration.proposal.yml').write_text(s)
(R/'ci.integration.proposal.diff').write_text(''.join(difflib.unified_diff(base.splitlines(True),s.splitlines(True),fromfile='a/.github/workflows/ci.yml',tofile='b/.github/workflows/ci.yml')))
p=subprocess.run(['/home/dev/.local/share/mise/installs/actionlint/1.7.7/actionlint',str(R/'ci.integration.proposal.yml')],capture_output=True,text=True,timeout=60)
(R/'integration-actionlint.log').write_text(p.stdout+p.stderr);assert p.returncode==0,p.stdout+p.stderr
print('Proposal only; actionlint passed; no installed workflow changed')
