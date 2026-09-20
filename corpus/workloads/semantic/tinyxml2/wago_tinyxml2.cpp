#include <cstdint>
#include <cstdarg>
#include <cstdio>
#include <cstring>
#include "tinyxml2.h"

/* The exercised API is memory-only. TinyXML2 also defines optional file and
 * stdout helpers in the same translation unit; local dead-end definitions keep
 * those unused helpers from turning this core module into a WASI command. */
extern "C" FILE *fopen(const char *, const char *) { return nullptr; }
extern "C" int fclose(FILE *) { return -1; }
extern "C" size_t fread(void *, size_t, size_t, FILE *) { return 0; }
extern "C" size_t fwrite(const void *, size_t, size_t, FILE *) { return 0; }
extern "C" int fseek(FILE *, long, int) { return -1; }
extern "C" long ftell(FILE *) { return -1; }
extern "C" int vfprintf(FILE *, const char *, va_list) { return -1; }
extern "C" void abort(void) { __builtin_trap(); }

static uint64_t fnv1a(const void *data, size_t len) {
    const unsigned char *p = static_cast<const unsigned char *>(data);
    uint64_t hash = UINT64_C(14695981039346656037);
    for (size_t i = 0; i < len; i++) { hash ^= p[i]; hash *= UINT64_C(1099511628211); }
    return hash;
}

extern "C" uint64_t tinyxml2_run() {
    static const char input[] =
        "<catalog version=\"3\"><book id=\"7\"><title>Railshot &amp; Friends</title>"
        "<price currency=\"USD\">19.95</price></book><book id=\"11\"><title>Wasm</title>"
        "<price currency=\"EUR\">23.50</price></book></catalog>";
    tinyxml2::XMLDocument doc;
    if (doc.Parse(input) != tinyxml2::XML_SUCCESS) return 0;
    auto *root = doc.FirstChildElement("catalog");
    if (!root || root->IntAttribute("version") != 3) return 0;
    int sum = 0;
    for (auto *book = root->FirstChildElement("book"); book; book = book->NextSiblingElement("book")) {
        sum += book->IntAttribute("id");
    }
    root->SetAttribute("sum", sum);
    auto *note = doc.NewElement("note");
    note->SetText("deterministic");
    root->InsertEndChild(note);
    uint64_t hash = fnv1a(root->Attribute("sum"), std::strlen(root->Attribute("sum")));
    for (auto *book = root->FirstChildElement("book"); book; book = book->NextSiblingElement("book")) {
        const char *title = book->FirstChildElement("title")->GetText();
        const char *currency = book->FirstChildElement("price")->Attribute("currency");
        hash ^= fnv1a(title, std::strlen(title));
        hash ^= fnv1a(currency, std::strlen(currency));
    }
    hash ^= fnv1a(note->GetText(), std::strlen(note->GetText()));
    return hash;
}
