export function materialMemoryWarning(r) {
 if(/Wazero|_wazero|Exec/.test(r.name)||!(r.candidate>r.base))return false;
 const added=r.candidate-r.base;
 return (r.p!==null&&r.p<.05&&((r.unit==='B/op'&&r.base>=65536&&r.delta>0.5)||(r.unit==='allocs/op'&&added>=8)))||
  (r.unit==='B/op'&&added>=131072)||(r.unit==='allocs/op'&&added>=128);
}
