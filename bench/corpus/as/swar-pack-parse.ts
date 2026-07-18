// Focused corpus fixture for broadword kernels used by utf-as and json-as.
// Keep these source shapes synchronized with utf-as/assembly/internal/swar.ts
// and json-as/assembly/util/swar-int.ts.

@inline
function pack4(x: u64): u32 {
  return <u32>((x & 0xff)
    | ((x >> 8) & 0xff00)
    | ((x >> 16) & 0xff0000)
    | ((x >> 24) & 0xff000000));
}

@inline
function parse4Unsafe(block: u64): u32 {
  const digits = block - 0x0030003000300030;
  const pairs = (digits * 10 + (digits >> 16)) & 0x0000ffff0000ffff;
  return <u32>((pairs * 0x0000006400000001) >> 32);
}

export function pack(x: u64): u32 {
  return pack4(x);
}

export function parse4(x: u64): u32 {
  return parse4Unsafe(x);
}

export function runN(n: i32): u64 {
  let sum: u64 = 0;
  for (let i = 0; i < n; i++) {
    const x = <u64>i;
    sum += pack4(0x0044004300420041 ^ x);
    sum += parse4Unsafe(0x0034003300320031 + (x & 7));
  }
  return sum;
}
