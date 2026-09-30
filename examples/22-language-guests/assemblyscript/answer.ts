@external("tutorial", "answer")
declare function answer(): i32;

export function run(): i32 {
  return answer();
}
