// blake3 crate: hash of a generated buffer.
fn main() {
    let mut d = vec![0u8; 1 << 16];
    for (i, b) in d.iter_mut().enumerate() { *b = (i as u32).wrapping_mul(2654435761).to_le_bytes()[0]; }
    println!("blake3:{}", blake3::hash(&d).to_hex());
}
