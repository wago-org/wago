// crc crate: table-driven CRC-32 over a generated buffer.
fn main() {
    let crc = crc::Crc::<u32>::new(&crc::CRC_32_ISO_HDLC);
    let mut d = vec![0u8; 100_000];
    for (i, b) in d.iter_mut().enumerate() { *b = (i * 7) as u8; }
    println!("crc32:{:08x}", crc.checksum(&d));
}
