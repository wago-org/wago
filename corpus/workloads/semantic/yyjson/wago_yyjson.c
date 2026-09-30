#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "yyjson.h"

static uint64_t fnv1a(const void *data, size_t len) {
    const unsigned char *p = (const unsigned char *)data;
    uint64_t hash = UINT64_C(14695981039346656037);
    for (size_t i = 0; i < len; i++) {
        hash ^= p[i];
        hash *= UINT64_C(1099511628211);
    }
    return hash;
}

uint64_t yyjson_run(void) {
    static const char input[] =
        "{\"users\":[{\"name\":\"Ada\",\"scores\":[7,11,13]},"
        "{\"name\":\"Linus\",\"scores\":[17,19]}],"
        "\"active\":true,\"meta\":{\"version\":3}}";
    yyjson_doc *doc = yyjson_read(input, sizeof(input) - 1, 0);
    if (!doc) return 0;

    yyjson_mut_doc *mut = yyjson_doc_mut_copy(doc, NULL);
    yyjson_doc_free(doc);
    if (!mut) return 0;

    yyjson_mut_val *root = yyjson_mut_doc_get_root(mut);
    yyjson_mut_val *users = yyjson_mut_obj_get(root, "users");
    yyjson_mut_val *first = yyjson_mut_arr_get(users, 0);
    yyjson_mut_val *scores = yyjson_mut_obj_get(first, "scores");
    if (!yyjson_mut_arr_add_int(mut, scores, 23) ||
        !yyjson_mut_obj_add_str(mut, root, "runtime", "wago")) {
        yyjson_mut_doc_free(mut);
        return 0;
    }

    size_t len = 0;
    char *out = yyjson_mut_write(mut, 0, &len);
    yyjson_mut_doc_free(mut);
    if (!out) return 0;
    uint64_t hash = fnv1a(out, len);
    free(out);
    return hash;
}
