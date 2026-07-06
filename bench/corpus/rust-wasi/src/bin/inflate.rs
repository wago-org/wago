// miniz_oxide: deflate then inflate a buffer, verify round-trip. Historically the
// inflate (decompress) path died under wago while compress worked.
use miniz_oxide::deflate::compress_to_vec;
use miniz_oxide::inflate::decompress_to_vec;

fn main() {
    let mut data = vec![0u8; 50_000];
    for (i, b) in data.iter_mut().enumerate() {
        *b = ((i * 31 + (i / 7)) % 251) as u8;
    }
    let compressed = compress_to_vec(&data, 6);
    let restored = decompress_to_vec(&compressed).expect("inflate failed");
    let ok = restored == data;
    // crude checksum of the restored buffer for a deterministic, verifiable line.
    let sum: u32 = restored.iter().fold(0u32, |a, &b| a.wrapping_mul(31).wrapping_add(b as u32));
    println!("inflate:{}:{}:{:08x}", ok, restored.len(), sum);
}
