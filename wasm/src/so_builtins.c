// Force-import So-generated builtins so wasm-ld doesn't GC them.
// The OPA wasm compiler patches in calls to these functions after linking.

#include "go/generated/strings.h"

__attribute__((used))
static void *(*so_builtins[])() = {
    (void *(*)())strings_StringsCount,
};
