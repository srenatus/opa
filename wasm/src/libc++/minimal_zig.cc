// Minimal C++ runtime for wasm32-wasi with Zig's libc++
// Provides essential runtime functions and explicit std::string instantiation

#include <cstddef>
#include <cstdarg>
#include <cstring>
#include <string>

extern "C" {
    // OPA's memory management functions
    void* opa_malloc(size_t size);
    void opa_free(void* ptr);
    [[noreturn]] void opa_abort(const char* msg);
    
    // C library functions that might be missing
    int isdigit(int c);
    char* strcpy(char* dest, const char* src);
}

// Operator new/delete implementations using OPA's allocator
void* operator new(size_t size) {
    return opa_malloc(size);
}

void operator delete(void* p) noexcept {
    opa_free(p);
}

void operator delete(void* p, size_t) noexcept {
    opa_free(p);
}

void* operator new[](size_t size) {
    return opa_malloc(size);
}

void operator delete[](void* p) noexcept {
    opa_free(p);
}

void operator delete[](void* p, size_t) noexcept {
    opa_free(p);
}

// Pure virtual function call handler
extern "C" void __cxa_pure_virtual() {
    opa_abort("pure virtual function called");
}

// Guard variables for static initialization (no-op for single-threaded WASM)
extern "C" int __cxa_guard_acquire(unsigned long long* guard) {
    if (*((unsigned char*)guard) == 0) {
        *((unsigned char*)guard) = 1;
        return 1;
    }
    return 0;
}

extern "C" void __cxa_guard_release(unsigned long long*) {
    // No-op in single-threaded environment
}

extern "C" void __cxa_guard_abort(unsigned long long*) {
    // No-op in single-threaded environment
}

// C++ stdlib functions needed by libc++
namespace std {
inline namespace __1 {

// Error handling function - called when assertions fail
[[noreturn]] void __libcpp_verbose_abort(const char* format, ...) noexcept {
    opa_abort("libc++ abort");
}

// Hash table helper - returns next prime number for hash table size
unsigned long __next_prime(unsigned long n) {
    // Simple prime number table for hash table sizing
    static const unsigned long primes[] = {
        2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47,
        53, 59, 61, 67, 71, 73, 79, 83, 89, 97, 103, 109, 113, 127,
        137, 139, 149, 157, 167, 179, 193, 199, 211, 227, 241, 257,
        277, 293, 313, 337, 359, 383, 409, 439, 467, 503, 541, 577,
        619, 661, 709, 761, 823, 887, 953, 1031, 1109, 1193, 1289,
        1381, 1493, 1613, 1741, 1879, 2029, 2179, 2357, 2549, 2753,
        2971, 3209, 3469, 3739, 4027, 4349, 4703, 5087, 5503, 5953,
        6427, 6949, 7517, 8123, 8783, 9497, 10273, 11113, 12011,
        12983, 14033, 15173, 16411, 17749, 19183, 20753, 22447,
        24251, 26171, 28277, 30577, 33023, 35731, 38873, 42043,
        45481, 49201, 53201, 57557, 62233, 67307, 72817, 78779,
        85229, 92203, 99733, 107897, 116731, 126271, 136607,
        147793, 159871, 172933, 187091, 202409, 218971, 236897,
        256279, 277261, 299951, 324503, 351061, 379787, 410857,
        444487, 480881, 520241, 562841, 608903, 658753, 712697,
        771049, 834181, 902483, 976369
    };
    static const unsigned long num_primes = sizeof(primes) / sizeof(primes[0]);
    
    // Find first prime >= n
    for (unsigned long i = 0; i < num_primes; i++) {
        if (primes[i] >= n) {
            return primes[i];
        }
    }
    
    // If n is larger than our table, return n itself (might not be prime, but close enough)
    return n;
}

// Sorting function used by re2
// Simple selection sort implementation
template<class Compare, class RandomAccessIterator>
void __sort(RandomAccessIterator first, RandomAccessIterator last, Compare comp) {
    if (first == last) return;
    
    for (RandomAccessIterator i = first; i != last; ++i) {
        RandomAccessIterator min = i;
        for (RandomAccessIterator j = i + 1; j != last; ++j) {
            // Use < directly instead of comp since __less is just a wrapper
            if (*j < *min) {
                min = j;
            }
        }
        if (min != i) {
            auto temp = *i;
            *i = *min;
            *min = temp;
        }
    }
}

// Explicit instantiation for the specific type used by re2
template void __sort<__less<int, int>&, int*>(int*, int*, __less<int, int>&);

// Call once implementation (no-op stub for single-threaded)
void __call_once(unsigned long volatile& flag, void* arg, void (*func)(void*)) {
    // Simple single-threaded implementation
    if (flag == 0) {
        flag = 1;
        func(arg);
        flag = 2;
    }
}

} // namespace __1
} // namespace std

// C library functions
extern "C" {

// Check if character is a digit
int isdigit(int c) {
    return (c >= '0' && c <= '9') ? 1 : 0;
}

// String copy
char* strcpy(char* dest, const char* src) {
    char* ret = dest;
    while ((*dest++ = *src++));
    return ret;
}

// Pthread stubs - we compile with -D_LIBCPP_HAS_NO_THREADS but re2 uses them directly
typedef struct {
    int dummy;
} pthread_rwlock_t;

typedef struct {
    int dummy;
} pthread_rwlockattr_t;

int pthread_rwlock_init(pthread_rwlock_t*, const pthread_rwlockattr_t*) {
    return 0; // Success stub
}

int pthread_rwlock_destroy(pthread_rwlock_t*) {
    return 0; // Success stub
}

int pthread_rwlock_rdlock(pthread_rwlock_t*) {
    return 0; // Success stub - no-op in single-threaded
}

int pthread_rwlock_wrlock(pthread_rwlock_t*) {
    return 0; // Success stub - no-op in single-threaded
}

int pthread_rwlock_unlock(pthread_rwlock_t*) {
    return 0; // Success stub - no-op in single-threaded
}

} // extern "C"

// Explicit template instantiation for std::string to provide all needed symbols
// This forces the compiler to generate all std::string methods
namespace std {
    template class basic_string<char>;
    template class basic_string<wchar_t>;
}
