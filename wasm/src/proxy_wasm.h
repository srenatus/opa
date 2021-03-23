#ifndef OPA_PROXY_WASM_H
#define OPA_PROXY_WASM_H

#include <stdio.h>

enum wasm_result_t
{ 
  Result_Ok,
  // The result could not be found, e.g. a provided key did not appear in a
  // table.
  Result_NotFound,
  // An argument was bad, e.g. did not not conform to the required range.
  Result_BadArgument,
  // A protobuf could not be serialized.
  Result_SerializationFailure,
  // A protobuf could not be parsed.
  Result_ParseFailure,
  // A provided expression (e.g. "foo.bar") was illegal or unrecognized.
  Result_BadExpression,
  // A provided memory range was not legal.
  Result_InvalidMemoryAccess,
  // Data was requested from an empty container.
  Result_Empty,
  // The provided CAS did not match that of the stored data.
  Result_CasMismatch,
  // Returned result was unexpected, e.g. of the incorrect size.
  Result_ResultMismatch,
  // Internal failure: trying check logs of the surrounding system.
  Result_InternalFailure,
  // The connection/stream/pipe was broken/closed unexpectedly.
  Result_BrokenConnection,
  // Feature not implemented.
  Result_Unimplemented
};
typedef enum wasm_result_t wasm_result_t;

enum proxy_map_type_t
{
  RequestHeaders,
  RequestTrailers,
  ResponseHeaders,
  ResponseTrailers,
  GrpcReceiveInitialMetadata,
  GrpcReceiveTrailingMetadata,
  HttpCallResponseHeaders,
  HttpCallResponseTrailers,

};
typedef enum proxy_map_type_t proxy_map_type_t;


enum filter_headers_status_t
{
  Status_Ok,
  Status_Stop,
  Status_ContinueAndEndStream,
  Status_StopAllIterationAndBuffer,
  Status_StopAllIterationAndWatermark
};
typedef enum filter_headers_status_t filter_headers_status_t;

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
wasm_result_t proxy_log(uint32_t log_level, const char *message_data, size_t message_size);
wasm_result_t proxy_get_buffer_bytes(uint32_t buffer_type, uint32_t offset, size_t max_size, const char* buffer_data, size_t *buffer_size);
wasm_result_t proxy_get_header_map_pairs(proxy_map_type_t header_type, const char **header_map, size_t *size);

#endif
