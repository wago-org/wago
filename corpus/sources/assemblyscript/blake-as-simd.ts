import { hashUnsafe } from "./blake3/index_simd";

const input = memory.data(4096);
const output = memory.data(32);

for (let i = 0; i < 4096; i++) store<u8>(input + <usize>i, <u8>(i % 251));

export function hashN(n: i32): i32 {
  for (let i = 0; i < n; i++) hashUnsafe(input, 4096, output);
  return load<i32>(output);
}
