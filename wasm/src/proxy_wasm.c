#include "malloc.h"
#include "std.h"
#include <stdio.h>

WASM_EXPORT(proxy_abi_version_0_2_0)
void proxy_abi_version_0_2_0(void)
{
}

WASM_EXPORT(malloc)
void *proxy_malloc(size_t len)
{
    return opa_malloc(len);
}

WASM_EXPORT(proxy_on_vm_start)
bool proxy_on_vm_start(uint32_t id, uint32_t len)
{
    return true;
}

WASM_EXPORT(proxy_on_configure)
bool proxy_on_configure(uint32_t id, uint32_t len)
{
    return true;
}

WASM_EXPORT(proxy_on_tick)
void proxy_on_tick(uint32_t id)
{
}

WASM_EXPORT(proxy_on_context_create)
void proxy_on_context_create(uint32_t id, uint32_t parent_id)
{
}

WASM_EXPORT(proxy_on_memory_allocate)
void proxy_on_memory_allocate(size_t memory_size, void *allocated_ptr)
{
}

WASM_EXPORT(proxy_on_done)
bool proxy_on_done(uint32_t id)
{
    return true;
}

WASM_EXPORT(proxy_on_log)
void proxy_on_log(uint32_t id)
{
}

WASM_EXPORT(proxy_on_delete)
void proxy_on_delete(uint32_t id)
{
}
