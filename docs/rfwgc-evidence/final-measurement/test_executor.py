"""Pure parser/pair tests. No process/benchmark/clock/lease access."""
import importlib.util,pathlib,unittest,tempfile,json,sys
from unittest.mock import patch
p=pathlib.Path(__file__).with_name('acquire.py');spec=importlib.util.spec_from_file_location('acquire',p);m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
class Contracts(unittest.TestCase):
 def test_invalid_before_lock(self):
  with tempfile.TemporaryDirectory() as d:
   root=pathlib.Path(d);seal=root/'seal.json';grant=root/'grant.json'
   seal.write_text(json.dumps({'status':'INVALID','candidate_valid':False}));grant.write_text('{}')
   with patch.object(sys,'argv',['acquire','--seal',str(seal),'--grant',str(grant),'--out',str(root/'out')]),patch.object(m.fcntl,'flock') as flock:
    with self.assertRaisesRegex(RuntimeError,'INVALID'):m.main()
    flock.assert_not_called();self.assertFalse((root/'out').exists())
 def test_testcycle_is_one_process_metric(self):
  got=m.parse('--- PASS: TestX/a (0.0s)\n--- PASS: TestX/b (0.0s)\n--- PASS: TestX (0.0s)','test-cycle',['TestX/a','TestX/b'],12)
  self.assertEqual(got,{'complete-test-cycle':12})
 def test_zero_leaf(self):
  with self.assertRaises(RuntimeError):m.parse('PASS','benchmark',[],1)
 def test_changed_leaf(self):
  with self.assertRaises(RuntimeError):m.parse('BenchmarkOther-8 1 100 ns/op','benchmark',['BenchmarkWanted'],1)
 def test_every_leaf(self):
  got=m.parse('BenchmarkX/a-8 4 100 ns/op\nBenchmarkX/b-8 4 200 ns/op','benchmark',['BenchmarkX/a','BenchmarkX/b'],1)
  self.assertEqual(got,{'BenchmarkX/a':100,'BenchmarkX/b':200})
 def test_parent_only(self):
  with self.assertRaises(RuntimeError):m.parse('--- PASS: TestX (0.0s)','test-cycle',['TestX/leaf'],1)
 def test_skip(self):
  with self.assertRaises(RuntimeError):m.parse('--- SKIP: TestX\n--- PASS: TestY','test-cycle',['TestY'],1)
 def test_controls_both_orders(self):
  for order in ['ABBA','BAAB']:m.controls([{'x':100} for _ in order],order)
 def test_one_leaf_rejects_block(self):
  with self.assertRaises(RuntimeError):m.controls([{'x':100,'y':100},{'x':100,'y':100},{'x':100,'y':111},{'x':100,'y':100}],'ABBA')
 def test_duplicate(self):
  with self.assertRaises(RuntimeError):m.parse('BenchmarkX-8 1 1 ns/op\nBenchmarkX-8 1 2 ns/op','benchmark',['BenchmarkX'],1)
if __name__=='__main__':unittest.main()
