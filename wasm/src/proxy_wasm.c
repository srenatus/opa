#include "proxy_wasm.h"
#include "malloc.h"
#include "value.h"
#include "std.h"
#include "json.h"
#include "stdlib.h"
#include "str.h"
#include <stdio.h>

#define LOG(str) proxy_log(LOG_LEVEL_WARN, str, sizeof(str))
#define LOG_V(val) proxy_log(LOG_LEVEL_WARN, val, opa_strlen(val))

WASM_EXPORT(proxy_abi_version_0_2_0)
void proxy_abi_version_0_2_0(void)
{
}

WASM_EXPORT(proxy_on_vm_start)
bool proxy_on_vm_start(uint32_t id, uint32_t len)
{
    LOG("on_vm_start");
    return true;
}

WASM_EXPORT(proxy_on_configure)
bool proxy_on_configure(uint32_t id, uint32_t len)
{
    LOG("on_configure");

    size_t buffer_size = len;
    const char* buffer_data = opa_malloc(len*sizeof(buffer_data));
    uint32_t res = proxy_get_buffer_bytes(BUFFER_PLUGIN_CONFIGURATION, 0, len, buffer_data, &buffer_size);
    proxy_log(LOG_LEVEL_WARN, buffer_data, buffer_size);
    return true;
}

WASM_EXPORT(proxy_on_tick)
void proxy_on_tick(uint32_t id)
{
    LOG("on_tick");
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

WASM_EXPORT(proxy_on_request_headers)
filter_headers_status_t proxy_on_request_headers(uint32_t id, size_t num_headers, bool end_of_stream)
{
    proxy_map_type_t map_type = RequestHeaders;
    const char *hdr_map = opa_malloc(num_headers*sizeof(hdr_map));
    wasm_result_t res = proxy_get_header_map_pairs(map_type, &hdr_map, &num_headers);
    if (res != Result_Ok) {
        LOG("res != OK");
        return Status_Stop;
    }
    LOG_V(opa_json_dump(opa_number_int(num_headers)));

    for (int i = 0; i < num_headers; i++)
    {
        LOG_V(opa_json_dump(opa_number_int(i)));
        LOG_V(hdr_map[i]);
    }
    LOG("on_http_request_headers OK");
    return Status_Ok;
}
