#ifndef OPA_PROXY_WASM_H
#define OPA_PROXY_WASM_H

#include <stdio.h>

#define WASM_RESULT_OK            0
#define WASM_RESULT_NOT_FOUND     1
#define WASM_RESULT_UNIMPLEMENTED 12

#define LOG_LEVEL_TRACE    0
#define LOG_LEVEL_INFO     1
#define LOG_LEVEL_WARN     2
#define LOG_LEVEL_ERROR    3
#define LOG_LEVEL_CRITICAL 4

#define BUFFER_HTTP_REQUEST_BODY       0
#define BUFFER_HTTP_RESPONSE_BODY      1
#define BUFFER_NETWORK_DOWNSTREAM_DATA 2
#define BUFFER_NETWORK_UPSTREAM_DATA   3
#define BUFFER_HTTP_CALL_RESPONSE_BODY 4
#define BUFFER_GRPC_RECEIVE_BUFFER     5
#define BUFFER_VM_CONFIGURATION        6
#define BUFFER_PLUGIN_CONFIGURATION    7
#define BUFFER_CALL_DATA               8

// imports
uint32_t proxy_log(uint32_t log_level, const char *message_data, size_t message_size);
uint32_t proxy_get_buffer_bytes(uint32_t buffer_type, uint32_t offset, size_t max_size, const char* buffer_data, size_t *buffer_size);

#endif
