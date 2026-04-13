#pragma once
#include "so/builtin/builtin.h"
#include "so/c/c.h"
#include "so/errors/errors.h"
#include "so/math/math.h"

// -- Embeds --

#include <assert.h>
#include <string.h>
#include "so/builtin/builtin.h"

// SwapByte swaps n bytes between a and b.
// Panics if either a or b is nil.
//
// SwapByte temporarily allocates a buffer of size n
// on the stack, so it's not suitable for large n.
static inline void mem_SwapByte(void* a, void* b, so_int n) {
    assert(a != NULL && "mem: nil pointer");
    assert(b != NULL && "mem: nil pointer");
    assert(n >= 0 && "mem: negative size");
    if (n == 0) return;

    size_t size = (size_t)n;
    char tmp[size];
    memcpy(tmp, a, size);
    memcpy(a, b, size);
    memcpy(b, tmp, size);
}

// -- Types --

// Allocator defines the interface for memory allocators.
// Whether allocated or reallocated memory is zeroed is allocator-specific.
typedef struct mem_Allocator {
    void* self;
    so_R_ptr_err (*Alloc)(void* self, so_int size, so_int align);
    void (*Free)(void* self, void* ptr, so_int size, so_int align);
    so_R_ptr_err (*Realloc)(void* self, void* ptr, so_int oldSize, so_int newSize, so_int align);
} mem_Allocator;

// A Stats records statistics about the memory allocator.
typedef struct mem_Stats {
    uint64_t Alloc;
    uint64_t TotalAlloc;
    uint64_t Mallocs;
    uint64_t Frees;
} mem_Stats;

// Arena is a memory allocator that bump-allocates linearly
// within a fixed buffer. Individual Free calls are no-ops.
// Use Reset to reclaim all memory at once.
typedef struct mem_Arena {
    so_Slice buf;
    so_int offset;
    so_int lastStart;
} mem_Arena;

// NoAllocator is an Allocator that returns an error
// on any allocation. Free is a no-op.
// It's meant for cases when allocations are strictly forbidden.
typedef struct mem_NoAllocator {
} mem_NoAllocator;

// SystemAllocator uses the system's malloc, realloc, and free functions.
// It zeros out new memory on allocation and reallocation.
typedef struct mem_SystemAllocator {
} mem_SystemAllocator;

// A Tracker wraps an Allocator and tracks all
// allocations and deallocations made through it.
typedef struct mem_Tracker {
    mem_Allocator Allocator;
    mem_Stats Stats;
} mem_Tracker;

// -- Variables and constants --

// ErrOutOfMemory is returned when a memory allocation
// fails due to insufficient memory.
extern so_Error mem_ErrOutOfMemory;
extern so_Error mem_ErrNoAlloc;

// NoAlloc is an instance of an Allocator that returns an error
// on any allocation. Free is a no-op.
// It's meant for cases when allocations are strictly forbidden.
extern mem_Allocator mem_NoAlloc;

// System is an instance of a memory allocator that uses
// the system's malloc, realloc, and free functions.
extern mem_Allocator mem_System;

// -- Functions and methods --

// NewArena creates an arena allocator backed by the given buffer.
mem_Arena mem_NewArena(so_Slice buf);
so_R_ptr_err mem_Arena_Alloc(void* self, so_int size, so_int align);
so_R_ptr_err mem_Arena_Realloc(void* self, void* ptr, so_int oldSize, so_int newSize, so_int align);
void mem_Arena_Free(void* self, void* ptr, so_int size, so_int align);

// Reset reclaims all allocated memory, allowing the arena to be reused.
void mem_Arena_Reset(void* self);

// Alloc allocates a single value of type T using allocator a.
// Returns a pointer to the allocated memory or panics on failure.
// Whether new memory is zeroed depends on the allocator.
// If the allocator is nil, uses the system allocator.
//
#define mem_Alloc(T, a_) ({ \
    so_R_ptr_err _res1 = mem_TryAlloc(T, (a_)); \
    T* _ptr = _res1.val; \
    so_Error _err = _res1.err; \
    if (_err != NULL) { \
        so_panic(_err->msg); \
    } \
    _ptr; \
})

// TryAlloc is like [Alloc] but returns an error
// instead of panicking on failure.
//
#define mem_TryAlloc(T, a_) ({ \
    mem_Allocator _a = a_; \
    if (_a.self == NULL) { \
        _a = mem_System; \
    } \
    so_R_ptr_err _res1 = _a.Alloc(_a.self, c_Sizeof(T), c_Alignof(T)); \
    void* _ptr = _res1.val; \
    so_Error _err = _res1.err; \
    (so_R_ptr_err){.val = c_PtrAs(T, (_ptr)), .err = _err}; \
})

// Free frees a value previously allocated with [Alloc] or [TryAlloc].
// If the allocator is nil, uses the system allocator.
//
#define mem_Free(T, a_, ptr_) do { \
    mem_Allocator _a = a_; \
    if (_a.self == NULL) { \
        _a = mem_System; \
    } \
    _a.Free(_a.self, ptr_, c_Sizeof(T), c_Alignof(T)); \
} while (0)

// AllocSlice allocates a slice of type T with given length
// and capacity using allocator a.
// Returns a slice of the allocated memory or panics on failure.
// Whether new memory is zeroed depends on the allocator.
// If the allocator is nil, uses the system allocator.
//
#define mem_AllocSlice(T, a_, len_, cap_) ({ \
    so_R_slice_err _res1 = mem_TryAllocSlice(T, (a_), (len_), (cap_)); \
    so_Slice _s = _res1.val; \
    so_Error _err = _res1.err; \
    if (_err != NULL) { \
        so_panic(_err->msg); \
    } \
    _s; \
})

// TryAllocSlice is like [AllocSlice] but returns an error
// instead of panicking on allocation failure.
//
#define mem_TryAllocSlice(T, a_, len_, cap_) ({ \
    mem_Allocator _a = a_; \
    if (_a.self == NULL) { \
        _a = mem_System; \
    } \
    so_int _len = len_, _cap = cap_; \
    so_int _esize = c_Sizeof(T), _align = c_Alignof(T); \
    c_Assert(_len >= 0, "mem: negative length"); \
    c_Assert(_cap >= 0, "mem: negative capacity"); \
    c_Assert(_len <= _cap, "mem: length exceeds capacity"); \
    c_Assert(_cap < math_MaxInt / _esize, "mem: capacity overflow"); \
    void* _ptr = NULL; \
    so_Error _err = NULL; \
    if (_cap > 0) { \
        so_R_ptr_err _res1 = _a.Alloc(_a.self, _esize * _cap, _align); \
        _ptr = _res1.val; \
        _err = _res1.err; \
    } else { \
        _ptr = &so_Nil; \
    } \
    so_Slice _ts = {&so_Nil, 0, 0}; \
    if (_err == NULL) { \
        _ts = c_Slice(T, (c_PtrAs(T, (_ptr))), (_len), (_cap)); \
    } \
    (so_R_slice_err){.val = _ts, .err = _err}; \
})

// ReallocSlice reallocates a slice of type T with new length and capacity
// using allocator a. Preserves contents up to the old capacity.
// Returns the reallocated slice or panics on failure.
// Whether new memory is zeroed depends on the allocator.
// If the allocator is nil, uses the system allocator.
//
#define mem_ReallocSlice(T, a_, slice_, newLen_, newCap_) ({ \
    so_R_slice_err _res1 = mem_TryReallocSlice(T, (a_), (slice_), (newLen_), (newCap_)); \
    so_Slice _s = _res1.val; \
    so_Error _err = _res1.err; \
    if (_err != NULL) { \
        so_panic(_err->msg); \
    } \
    _s; \
})

// TryReallocSlice is like [ReallocSlice] but returns an error
// instead of panicking on allocation failure.
//
#define mem_TryReallocSlice(T, a_, slice_, newLen_, newCap_) ({ \
    mem_Allocator _a = a_; \
    if (_a.self == NULL) { \
        _a = mem_System; \
    } \
    so_int _oldCap = so_cap(slice_); \
    so_int _newLen = newLen_, _newCap = newCap_; \
    so_int _esize = c_Sizeof(T), _align = c_Alignof(T); \
    c_Assert(_newLen >= 0, "mem: negative length"); \
    c_Assert(_newCap >= 0, "mem: negative capacity"); \
    c_Assert(_newLen <= _newCap, "mem: length exceeds capacity"); \
    c_Assert(_newCap < math_MaxInt / _esize, "mem: capacity overflow"); \
    void* _newPtr = NULL; \
    so_Error _err = NULL; \
    if (_newCap == 0) { \
        if (_oldCap > 0) { \
            _a.Free(_a.self, unsafe_SliceData(slice_), _esize * _oldCap, _align); \
        } \
        _newPtr = &so_Nil; \
    } else if (_oldCap == 0) { \
        so_R_ptr_err _res1 = _a.Alloc(_a.self, _esize * _newCap, _align); \
        _newPtr = _res1.val; \
        _err = _res1.err; \
    } else { \
        T* ptr = unsafe_SliceData(slice_); \
        so_R_ptr_err _res2 = _a.Realloc(_a.self, ptr, _esize * _oldCap, _esize * _newCap, _align); \
        _newPtr = _res2.val; \
        _err = _res2.err; \
    } \
    so_Slice _s = {&so_Nil, 0, 0}; \
    if (_err == NULL) { \
        _s = c_Slice(T, (c_PtrAs(T, (_newPtr))), (_newLen), (_newCap)); \
    } \
    (so_R_slice_err){.val = _s, .err = _err}; \
})

// FreeSlice frees a slice previously allocated with [AllocSlice] or [TryAllocSlice].
// If the allocator is nil, uses the system allocator.
// Calling FreeSlice on an empty or nil slice is a no-op.
//
#define mem_FreeSlice(T, a_, slice_) do { \
    mem_Allocator _a = a_; \
    if (_a.self == NULL) { \
        _a = mem_System; \
    } \
    so_Slice _s = slice_; \
    so_int _cap = so_cap(_s); \
    if (_cap > 0) { \
        _a.Free(_a.self, unsafe_SliceData(_s), c_Sizeof(T) * _cap, c_Alignof(T)); \
    } \
} while (0)

// FreeString frees a heap-allocated string.
// If the allocator is nil, uses the system allocator.
void mem_FreeString(mem_Allocator a, so_String s);

// Clear zeroes size bytes starting at ptr.
//
static inline void mem_Clear(void* ptr, so_int size) {
    c_Assert(ptr != NULL, "mem: nil pointer");
    c_Assert(size >= 0, "mem: negative size");
    memset(ptr, 0, (uintptr_t)(size));
}

// Copy copies n bytes from src to dst. Returns dst.
// The memory areas must not overlap.
// Panics if either dst or src is nil.
//
static inline void* mem_Copy(void* dst, void* src, so_int n) {
    c_Assert(dst != NULL, "mem: nil pointer");
    c_Assert(src != NULL, "mem: nil pointer");
    c_Assert(n >= 0, "mem: negative size");
    return memcpy(dst, src, (uintptr_t)(n));
}

// Move copies n bytes from src to dst. Returns dst.
// The memory areas may overlap.
// Panics if either dst or src is nil.
//
static inline void* mem_Move(void* dst, void* src, so_int n) {
    c_Assert(dst != NULL, "mem: nil pointer");
    c_Assert(src != NULL, "mem: nil pointer");
    c_Assert(n >= 0, "mem: negative size");
    return memmove(dst, src, (uintptr_t)(n));
}

// Swap swaps the values pointed to by a and b.
// Panics if either a or b is nil.
//
#define mem_Swap(T, a_, b_) do { \
    c_Assert(a_ != NULL, "mem: nil pointer"); \
    c_Assert(b_ != NULL, "mem: nil pointer"); \
    T _tmp = *a_; \
    *a_ = *b_; \
    *b_ = _tmp; \
} while (0)
so_R_ptr_err mem_NoAllocator_Alloc(void* self, so_int size, so_int align);
so_R_ptr_err mem_NoAllocator_Realloc(void* self, void* ptr, so_int oldSize, so_int newSize, so_int align);
void mem_NoAllocator_Free(void* self, void* ptr, so_int size, so_int align);
so_R_ptr_err mem_SystemAllocator_Alloc(void* self, so_int size, so_int align);
so_R_ptr_err mem_SystemAllocator_Realloc(void* self, void* ptr, so_int oldSize, so_int newSize, so_int align);
void mem_SystemAllocator_Free(void* self, void* ptr, so_int size, so_int align);
so_R_ptr_err mem_Tracker_Alloc(void* self, so_int size, so_int align);
so_R_ptr_err mem_Tracker_Realloc(void* self, void* ptr, so_int oldSize, so_int newSize, so_int align);
void mem_Tracker_Free(void* self, void* ptr, so_int size, so_int align);
