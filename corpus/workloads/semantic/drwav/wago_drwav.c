#include <stdint.h>
#include <string.h>
#define DR_WAV_NO_STDIO
#define DR_WAV_IMPLEMENTATION
#include "dr_wav.h"

static void put16(uint8_t *p, uint16_t value) { p[0] = value & 255; p[1] = value >> 8; }
static void put32(uint8_t *p, uint32_t value) { put16(p, value & 65535); put16(p + 2, value >> 16); }

uint64_t drwav_run(void) {
    enum { frames = 2048, channels = 2, data_size = frames * channels * 2 };
    static uint8_t wav[44 + data_size];
    memcpy(wav, "RIFF", 4); put32(wav + 4, 36 + data_size); memcpy(wav + 8, "WAVEfmt ", 8);
    put32(wav + 16, 16); put16(wav + 20, 1); put16(wav + 22, channels); put32(wav + 24, 48000);
    put32(wav + 28, 48000 * channels * 2); put16(wav + 32, channels * 2); put16(wav + 34, 16);
    memcpy(wav + 36, "data", 4); put32(wav + 40, data_size);
    for (uint32_t i = 0; i < frames; i++) {
        int16_t left = (int16_t)((i * 257u) ^ (i << 5));
        int16_t right = (int16_t)(-left / 2 + (int16_t)(i * 3u));
        put16(wav + 44 + i * 4, (uint16_t)left); put16(wav + 46 + i * 4, (uint16_t)right);
    }
    drwav decoder;
    if (!drwav_init_memory(&decoder, wav, sizeof(wav), NULL)) return 1;
    if (decoder.channels != channels || decoder.sampleRate != 48000 || decoder.totalPCMFrameCount != frames) return 2;
    int16_t samples[channels * 257];
    uint64_t hash = UINT64_C(14695981039346656037);
    uint64_t total = 0;
    for (;;) {
        drwav_uint64 got = drwav_read_pcm_frames_s16(&decoder, 257, samples);
        if (got == 0) break;
        total += got;
        for (size_t i = 0; i < (size_t)got * channels; i++) hash = (hash ^ (uint16_t)samples[i]) * UINT64_C(1099511628211);
    }
    if (total != frames || !drwav_seek_to_pcm_frame(&decoder, 1000)) return 3;
    if (drwav_read_pcm_frames_s16(&decoder, 1, samples) != 1) return 4;
    hash = (hash ^ (uint16_t)samples[0]) * UINT64_C(1099511628211);
    drwav_uninit(&decoder);
    return hash;
}
