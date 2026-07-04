// base64 crate: encode + decode roundtrip.
use base64::{engine::general_purpose::STANDARD, Engine};
fn main() {
    let mut d = Vec::new();
    for i in 0..30_000u32 { d.push((i ^ (i >> 5)) as u8); }
    let e = STANDARD.encode(&d);
    assert_eq!(STANDARD.decode(&e).unwrap(), d);
    println!("base64:{}", e.len());
}
