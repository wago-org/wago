#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "cJSON.h"

static uint64_t hash_bytes(uint64_t hash, const unsigned char *data, size_t len) {
    for (size_t i = 0; i < len; i++) hash = (hash ^ data[i]) * UINT64_C(1099511628211);
    return hash;
}

uint64_t cjson_run(void) {
    static const char input[] =
        "{\"name\":\"wago\",\"enabled\":true,\"values\":[3,1,4,1,5,9],"
        "\"nested\":{\"unicode\":\"lambda \\u03bb\",\"count\":7}}";
    cJSON *root = cJSON_ParseWithLength(input, sizeof(input) - 1);
    if (!root) return 1;
    cJSON *values = cJSON_GetObjectItemCaseSensitive(root, "values");
    cJSON *nested = cJSON_GetObjectItemCaseSensitive(root, "nested");
    if (!cJSON_IsArray(values) || !cJSON_IsObject(nested)) { cJSON_Delete(root); return 2; }
    int sum = 0;
    cJSON *item = NULL;
    cJSON_ArrayForEach(item, values) sum += item->valueint;
    cJSON_ReplaceItemInObjectCaseSensitive(nested, "count", cJSON_CreateNumber(sum));
    cJSON_AddStringToObject(root, "status", "parsed-edited-serialized");
    char *printed = cJSON_PrintUnformatted(root);
    if (!printed) { cJSON_Delete(root); return 3; }
    uint64_t hash = hash_bytes(UINT64_C(14695981039346656037),
        (const unsigned char *)printed, strlen(printed));
    free(printed);
    cJSON_Delete(root);
    return hash;
}
