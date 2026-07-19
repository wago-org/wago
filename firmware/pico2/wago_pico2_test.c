#include "wago_pico2.h"

#include <stdio.h>
#include <string.h>

#define CHECK(condition) do { if (!(condition)) { fprintf(stderr, "check failed at line %d: %s\n", __LINE__, #condition); return 1; } } while (0)

_Static_assert(sizeof(struct wago_pico2_call_abi) == WAGO_PICO2_CALL_ABI_BYTES, "call ABI size");
_Static_assert(sizeof(struct wago_pico2_f32_frame) == 32, "f32 helper frame size");
_Static_assert(sizeof(struct wago_pico2_f64_frame) == 32, "f64 helper frame size");
_Static_assert(sizeof(struct wago_pico2_i64_frame) == 36, "i64 helper frame size");
_Static_assert(sizeof(struct wago_pico2_simd_frame) == 120, "SIMD helper frame size");
_Static_assert(sizeof(struct wago_pico2_helper_entries) == WAGO_PICO2_HELPER_TABLE_BYTES,
               "helper table size");

struct test_state {
    uint32_t instantiates;
    uint32_t starts;
    uint32_t calls;
    uint32_t cancels;
    uint32_t resets;
    uint32_t context;
    wago_pico2_code call_code;
};

static uint32_t get32(const uint8_t *p) {
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) |
           ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}

static void put16(uint8_t *p, uint16_t value) {
    p[0] = (uint8_t)value;
    p[1] = (uint8_t)(value >> 8);
}

static void put32(uint8_t *p, uint32_t value) {
    p[0] = (uint8_t)value;
    p[1] = (uint8_t)(value >> 8);
    p[2] = (uint8_t)(value >> 16);
    p[3] = (uint8_t)(value >> 24);
}

static uint32_t request(uint8_t *out, uint16_t kind, uint32_t sequence,
                        const uint8_t *payload, uint32_t payload_length) {
    uint32_t total = WAGO_PICO2_TRANSPORT_HEADER_BYTES + payload_length;
    put32(out + 0, WAGO_PICO2_TRANSPORT_MAGIC);
    put16(out + 4, WAGO_PICO2_TRANSPORT_VERSION);
    put16(out + 6, kind);
    put32(out + 8, sequence);
    put32(out + 12, payload_length);
    put32(out + 16, WAGO_PICO2_OK);
    if (payload_length != 0) {
        memcpy(out + WAGO_PICO2_TRANSPORT_HEADER_BYTES, payload, payload_length);
    }
    put32(out + 20, wago_pico2_crc32(out, 20,
                                     out + WAGO_PICO2_TRANSPORT_HEADER_BYTES,
                                     payload_length));
    return total;
}

static wago_pico2_code test_instantiate(void *user, struct wago_pico2_runner *runner) {
    struct test_state *state = (struct test_state *)user;
    (void)runner;
    state->instantiates++;
    return WAGO_PICO2_OK;
}

static wago_pico2_code test_start(void *user, uint32_t entry, uint32_t context) {
    struct test_state *state = (struct test_state *)user;
    CHECK(entry == 0x20000201u);
    state->starts++;
    state->context = context;
    return WAGO_PICO2_OK;
}

static wago_pico2_code test_call(void *user, uint32_t entry, uint32_t context,
                                 const uint32_t *parameters, uint32_t parameter_slots,
                                 uint32_t *results, uint32_t result_slots) {
    struct test_state *state = (struct test_state *)user;
    uint32_t i;
    CHECK(entry == 0x20000301u);
    state->calls++;
    state->context = context;
    if (state->call_code != WAGO_PICO2_OK) {
        return state->call_code;
    }
    for (i = 0; i < result_slots; ++i) {
        results[i] = 40u + i + (i < parameter_slots ? parameters[i] : 0u);
    }
    return WAGO_PICO2_OK;
}

static wago_pico2_code test_cancel(void *user, uint32_t context) {
    struct test_state *state = (struct test_state *)user;
    state->cancels++;
    state->context = context;
    return WAGO_PICO2_OK;
}

static wago_pico2_code test_reset(void *user, struct wago_pico2_runner *runner) {
    struct test_state *state = (struct test_state *)user;
    (void)runner;
    state->resets++;
    return WAGO_PICO2_OK;
}

int main(void) {
    struct test_state state = {0};
    const struct wago_pico2_invoker invoker = {
        &state,
        test_instantiate,
        test_start,
        test_call,
        test_cancel,
        test_reset,
    };
    const struct wago_pico2_function functions[] = {
        {0x20000301u, 0x20000400u, 2, 2},
    };
    struct wago_pico2_runner runner = {
        .target = WAGO_PICO2_TARGET_ARM32,
        .maximum_payload = 128,
        .context_address = 0x20000100u,
        .start_address = 0x20000201u,
        .functions = functions,
        .function_count = 1,
        .invoker = &invoker,
    };
    uint32_t parameters[4] = {0};
    uint32_t results[4] = {0};
    uint8_t scratch[128] = {0};
    struct wago_pico2_endpoint endpoint = {
        &runner,
        parameters,
        4,
        results,
        4,
        scratch,
        sizeof(scratch),
        128,
    };
    uint8_t request_bytes[160] = {0};
    uint8_t response[160] = {0};
    uint8_t payload[32] = {0};
    uint32_t request_length;
    uint32_t response_length = 0;
    int dispatch;

    request_length = request(request_bytes, WAGO_PICO2_HELLO, 7, NULL, 0);
    dispatch = wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                                   response, sizeof(response), &response_length);
    CHECK(dispatch == WAGO_PICO2_DISPATCH_OK);
    CHECK(response_length == WAGO_PICO2_TRANSPORT_HEADER_BYTES + WAGO_PICO2_TRANSPORT_HELLO_BYTES);
    CHECK(get32(response + 8) == 7);
    CHECK(get32(response + 12) == WAGO_PICO2_TRANSPORT_HELLO_BYTES);
    CHECK(get32(response + 16) == WAGO_PICO2_OK);
    CHECK(get32(response + WAGO_PICO2_TRANSPORT_HEADER_BYTES + 0) == WAGO_PICO2_TARGET_ARM32);
    CHECK(get32(response + WAGO_PICO2_TRANSPORT_HEADER_BYTES + 4) == WAGO_PICO2_CONTEXT_ABI_BYTES);
    CHECK(get32(response + WAGO_PICO2_TRANSPORT_HEADER_BYTES + 8) == WAGO_PICO2_CALL_ABI_BYTES);

    request_length = request(request_bytes, WAGO_PICO2_INSTANTIATE, 8, NULL, 0);
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, sizeof(response), &response_length) == WAGO_PICO2_DISPATCH_OK);
    CHECK(state.instantiates == 1 && runner.initialized && !runner.started);

    request_length = request(request_bytes, WAGO_PICO2_START, 9, NULL, 0);
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, sizeof(response), &response_length) == WAGO_PICO2_DISPATCH_OK);
    CHECK(state.starts == 1 && runner.started && state.context == runner.context_address);

    put32(payload + 0, 0);
    put32(payload + 4, 2);
    put32(payload + 8, 2);
    put32(payload + 12, 1);
    put32(payload + 16, 2);
    request_length = request(request_bytes, WAGO_PICO2_CALL, 10, payload, 20);
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, WAGO_PICO2_TRANSPORT_HEADER_BYTES + 7,
                              &response_length) == WAGO_PICO2_DISPATCH_CAPACITY);
    CHECK(state.calls == 0);
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, sizeof(response), &response_length) == WAGO_PICO2_DISPATCH_OK);
    CHECK(state.calls == 1 && state.context == functions[0].context);
    CHECK(response_length == WAGO_PICO2_TRANSPORT_HEADER_BYTES + 8);
    CHECK(get32(response + WAGO_PICO2_TRANSPORT_HEADER_BYTES + 0) == 41);
    CHECK(get32(response + WAGO_PICO2_TRANSPORT_HEADER_BYTES + 4) == 43);

    state.call_code = 3;
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, sizeof(response), &response_length) == WAGO_PICO2_DISPATCH_OK);
    CHECK(response_length == WAGO_PICO2_TRANSPORT_HEADER_BYTES);
    CHECK(get32(response + 16) == 3);

    request_bytes[20] ^= 1;
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, sizeof(response), &response_length) == WAGO_PICO2_DISPATCH_CHECKSUM);
    request_bytes[20] ^= 1;

    request_length = request(request_bytes, WAGO_PICO2_CANCEL, 11, NULL, 0);
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, sizeof(response), &response_length) == WAGO_PICO2_DISPATCH_OK);
    CHECK(state.cancels == 1);

    request_length = request(request_bytes, WAGO_PICO2_RESET, 12, NULL, 0);
    CHECK(wago_pico2_dispatch(&endpoint, request_bytes, request_length,
                              response, sizeof(response), &response_length) == WAGO_PICO2_DISPATCH_OK);
    CHECK(state.resets == 1 && !runner.initialized && !runner.started);

    {
        uint8_t initial_image[160] = {0};
        uint8_t live_image[160] = {0};
        const uint32_t contexts[] = {0x10000020u};
        const struct wago_pico2_helper_entries helpers = {
            0x1001u, 0x2001u, 0x3001u, 0x4001u,
        };
        const struct wago_pico2_image image = {
            .target = WAGO_PICO2_TARGET_ARM32,
            .maximum_payload = 128,
            .load_address = 0x10000000u,
            .image = live_image,
            .initial_image = initial_image,
            .image_size = sizeof(initial_image),
            .context_address = contexts[0],
            .contexts = contexts,
            .context_count = 1,
        };
        struct wago_pico2_runner embedded_runner;
        put32(initial_image + 0x20 + WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET,
              0x10000080u);
        memset(&embedded_runner, 0xa5, sizeof(embedded_runner));
        CHECK(wago_pico2_runner_init(&embedded_runner, &image, &helpers) == WAGO_PICO2_OK);
        CHECK(!embedded_runner.initialized && !embedded_runner.started);
        live_image[0] = 0xee;
        put32(initial_image + 0x20 + WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET,
              0x20000000u);
        CHECK(wago_pico2_runner_instantiate(&embedded_runner) == WAGO_PICO2_STATE);
        CHECK(live_image[0] == 0xee && !embedded_runner.initialized);
        put32(initial_image + 0x20 + WAGO_PICO2_CONTEXT_HELPER_TABLE_OFFSET,
              0x10000080u);
        CHECK(wago_pico2_runner_instantiate(&embedded_runner) == WAGO_PICO2_OK);
        CHECK(get32(live_image + 0x80 + WAGO_PICO2_HELPER_F64_OFFSET) == helpers.f64);
        CHECK(get32(live_image + 0x80 + WAGO_PICO2_HELPER_SIMD_OFFSET) == helpers.simd);
        CHECK(get32(live_image + 0x80 + WAGO_PICO2_HELPER_I64_OFFSET) == helpers.i64);
        CHECK(get32(live_image + 0x80 + WAGO_PICO2_HELPER_F32_OFFSET) == helpers.f32);
        live_image[0] = 0xff;
        CHECK(wago_pico2_runner_reset(&embedded_runner) == WAGO_PICO2_OK);
        CHECK(live_image[0] == initial_image[0]);
        CHECK(get32(live_image + 0x80 + WAGO_PICO2_HELPER_F64_OFFSET) == helpers.f64);
    }

    return 0;
}
