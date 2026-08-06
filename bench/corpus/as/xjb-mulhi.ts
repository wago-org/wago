// Focused corpus fixture for xjb-as's open-coded unsigned 64x64 multiply-high.
// Keep this source shape synchronized with xjb-as/assembly/xjb.ts: Railshot's
// bounded idiom matcher is tested against the Wasm that AssemblyScript emits.

function mulhi64(a: u64, b: u64): u64 {
  const a0 = a & 0xffffffff, a1 = a >> 32;
  const b0 = b & 0xffffffff, b1 = b >> 32;
  const w0 = a0 * b0;
  const t = a1 * b0 + (w0 >> 32);
  let w1 = t & 0xffffffff;
  const w2 = t >> 32;
  w1 = a0 * b1 + w1;
  return a1 * b1 + w2 + (w1 >> 32);
}

export function mulhi(a: u64, b: u64): u64 {
  return mulhi64(a, b);
}

export function runN(n: i32): u64 {
  let sum: u64 = 0;
  for (let i = 0; i < n; i++) {
    const j = <u64>i;
    sum ^= mulhi64(0x9e3779b97f4a7c15 + j, 0xd6e8feb86659fd93 ^ j * 0xa0761d6478bd642f);
  }
  return sum;
}
