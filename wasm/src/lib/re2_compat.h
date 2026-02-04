#ifndef RE2_COMPAT_H
#define RE2_COMPAT_H

// Minimal compatibility for compiling re2 with Zig's libc++ for wasm32-freestanding
// This provides only the missing pieces that Zig's freestanding environment lacks

#ifdef __cplusplus
extern "C" {
#endif

// FP classification macros (needed by Zig's math.h)
#ifndef FP_NAN
#define FP_NAN 0
#define FP_INFINITE 1
#define FP_ZERO 2
#define FP_SUBNORMAL 3
#define FP_NORMAL 4
#endif

// div_t types (needed by Zig's stdlib.h)
#ifndef _DIV_T_DEFINED
typedef struct { int quot; int rem; } div_t;
typedef struct { long quot; long rem; } ldiv_t;
typedef struct { long long quot; long long rem; } lldiv_t;
#define _DIV_T_DEFINED
#endif

// mbstate_t (needed by libc++ for fpos<>)
#if !defined(_MBSTATE_T_DEFINED) && !defined(__mbstate_t_defined)
typedef struct { unsigned int __opaque1, __opaque2; } mbstate_t;
#define _MBSTATE_T_DEFINED
#define __mbstate_t_defined
#endif

// String functions that re2 needs
void *memcpy(void *__restrict dest, const void *__restrict src, unsigned long n);
void *memmove(void *dest, const void *src, unsigned long n);
void *memset(void *s, int c, unsigned long n);
int memcmp(const void *s1, const void *s2, unsigned long n);
unsigned long strlen(const char *s);

// Define EOF
#ifndef EOF
#define EOF (-1)
#endif

#ifdef __cplusplus
}
#endif

#endif // RE2_COMPAT_H
