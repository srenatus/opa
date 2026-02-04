#ifndef OPA_STDDEF_H
#define OPA_STDDEF_H

#if defined(__has_include)
#  if __has_include_next(<stddef.h>)
#    include_next <stddef.h>
#  else
#    define OPA_NEED_STDDEF_DECLS
#  endif
#else
#  define OPA_NEED_STDDEF_DECLS
#endif

#ifdef OPA_NEED_STDDEF_DECLS
#define NULL ((void *)0)

typedef __SIZE_TYPE__ size_t;
typedef __PTRDIFF_TYPE__ ptrdiff_t;
typedef __WCHAR_TYPE__ wchar_t;

#if defined(__NEED_max_align_t)
typedef struct {
    long long __ll;
    long double __ld;
} max_align_t;
#endif

#define offsetof(type, member) __builtin_offsetof(type, member)
#endif

#endif
