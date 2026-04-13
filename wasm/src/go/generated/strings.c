#include "strings.h"
#include "value.h"

// -- Variables and constants --
static const so_int opaStringType = 4;

// -- Forward declarations --
static so_String toSoString(void* v);

// -- Implementation --

// toSoString converts an opa_string_t pointer to a so_String.
// Since hdr is the first field, a void* can be cast directly to *opaString.
static so_String toSoString(void* v) {
    opa_string_t* s = c_PtrAs(opa_string_t, (v));
    so_byte* p = c_PtrAs(so_byte, (s->v));
    so_Slice bs = c_Bytes(p, (so_int)(s->len));
    return so_bytes_string(bs);
}

void* strings_StringsCount(void* a, void* b) {
    if (opa_value_type(a) != opaStringType || opa_value_type(b) != opaStringType) {
        return NULL;
    }
    so_int n = strings_Count(toSoString(a), toSoString(b));
    return opa_number_int((int64_t)(n));
}
