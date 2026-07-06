// num-bigint crate: big-integer factorial printed via to_str_radix (the path
// that hit a guard-page-only miscompile: `assertion failed: digit_2 < big_base`).
use num_bigint::BigUint;
use num_traits::One;

fn factorial(n: u32) -> BigUint {
    let mut acc = BigUint::one();
    for i in 2..=n {
        acc *= i;
    }
    acc
}

fn main() {
    let f = factorial(500);
    let s = f.to_str_radix(10);
    // Deterministic, verifiable summary: length + first/last digits.
    println!("bignum:{}:{}:{}", s.len(), &s[..8], &s[s.len() - 8..]);
}
