#include "wago_pico2.h"

#ifdef WAGO_PICO2_PICO_SDK

#include "pico/stdlib.h"

static int wago_pico_sdk_read(void *user, uint8_t *destination, uint32_t length) {
    uint32_t i;
    (void)user;
    for (i = 0; i < length; ++i) {
        int value;
        do {
            value = getchar_timeout_us(1000);
        } while (value == PICO_ERROR_TIMEOUT);
        if (value < 0) {
            return -1;
        }
        destination[i] = (uint8_t)value;
    }
    return (int)length;
}

static int wago_pico_sdk_write(void *user, const uint8_t *source, uint32_t length) {
    uint32_t i;
    (void)user;
    for (i = 0; i < length; ++i) {
        putchar_raw(source[i]);
    }
    stdio_flush();
    return (int)length;
}

int wago_pico2_pico_sdk_serve(struct wago_pico2_endpoint *endpoint,
                              uint8_t *request, uint32_t request_capacity,
                              uint8_t *response, uint32_t response_capacity) {
    const struct wago_pico2_io io = {
        NULL,
        wago_pico_sdk_read,
        wago_pico_sdk_write,
    };
    for (;;) {
        int result = wago_pico2_serve_once(endpoint, &io, request, request_capacity,
                                           response, response_capacity);
        if (result != WAGO_PICO2_DISPATCH_OK) {
            return result;
        }
    }
}

#endif
