package strings

import (
	"solod.dev/so/c"
	"solod.dev/so/strings"
)

// OPA value types. These are defined in value.h.

//so:include "value.h"

//so:extern opa_value
type opaValue struct {
	typ uint8
}

// opa_string_t layout from value.h:
//
//	typedef struct {
//	    opa_value hdr;           // 1 byte (unsigned char type)
//	    unsigned char free;      // 1 byte
//	    size_t len;              // platform-dependent (4 on wasm32)
//	    const char *v;           // pointer
//	} opa_string_t;
//
//so:extern opa_string_t
type opaString struct {
	hdr  opaValue
	free uint8
	len  uintptr // size_t
	v    *byte
}

// OPA functions from value.h / value.c.

//so:extern opa_value_type nodecay
func opaValueType(node any) int32

//so:extern opa_number_int
func opaNumberInt(v int64) any

const opaStringType = 4

// toSoString converts an opa_string_t pointer to a so_String.
// Since hdr is the first field, a void* can be cast directly to *opaString.
func toSoString(v any) string {
	s := c.PtrAs[opaString](v)
	p := c.PtrAs[byte](s.v)
	bs := c.Bytes(p, int(s.len))
	return string(bs)
}

func StringsCount(a any, b any) any {
	if opaValueType(a) != opaStringType || opaValueType(b) != opaStringType {
		return nil
	}
	n := strings.Count(toSoString(a), toSoString(b))
	return opaNumberInt(int64(n))
}
