#include <stdint.h>
#include <string.h>
#include "monocypher.h"

static uint64_t hash_bytes(uint64_t hash, const uint8_t *data, size_t len) {
    for (size_t i = 0; i < len; i++) hash = (hash ^ data[i]) * UINT64_C(1099511628211);
    return hash;
}

uint64_t monocypher_run(void) {
    uint8_t key[32], nonce[24], ad[37], plain[4096], cipher[4096], opened[4096], mac[16], digest[64];
    for (size_t i = 0; i < sizeof(key); i++) key[i] = (uint8_t)(i * 9u + 5u);
    for (size_t i = 0; i < sizeof(nonce); i++) nonce[i] = (uint8_t)(i * 13u + 1u);
    for (size_t i = 0; i < sizeof(ad); i++) ad[i] = (uint8_t)(i * 7u + 3u);
    for (size_t i = 0; i < sizeof(plain); i++) plain[i] = (uint8_t)((i * 29u + (i >> 4)) & 255u);
    crypto_aead_lock(cipher, mac, key, nonce, ad, sizeof(ad), plain, sizeof(plain));
    if (crypto_aead_unlock(opened, mac, key, nonce, ad, sizeof(ad), cipher, sizeof(cipher)) != 0) return 1;
    if (memcmp(plain, opened, sizeof(plain)) != 0) return 2;
    crypto_blake2b_ctx ctx;
    crypto_blake2b_init(&ctx, sizeof(digest));
    crypto_blake2b_update(&ctx, plain, 17);
    crypto_blake2b_update(&ctx, plain + 17, 1009);
    crypto_blake2b_update(&ctx, plain + 1026, sizeof(plain) - 1026);
    crypto_blake2b_final(&ctx, digest);
    uint64_t hash = hash_bytes(UINT64_C(14695981039346656037), cipher, sizeof(cipher));
    hash = hash_bytes(hash, mac, sizeof(mac));
    return hash_bytes(hash, digest, sizeof(digest));
}
