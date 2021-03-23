// Copyright 2016-2020 Envoy Project Authors
// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

#include <string>
#include <string_view>
#include <unordered_map>

#include "proxy_wasm_intrinsics.h"
#include "context.h"
#include "json.h"

class ExampleRootContext : public RootContext {
public:
  explicit ExampleRootContext(uint32_t id, std::string_view root_id) : RootContext(id, root_id) {}

  bool onStart(size_t) override;
  bool onConfigure(size_t) override;
  void onTick() override;
};

class ExampleContext : public Context {
public:
  explicit ExampleContext(uint32_t id, RootContext *root) : Context(id, root) {}

  void onCreate() override;
  FilterHeadersStatus onRequestHeaders(uint32_t headers, bool end_of_stream) override;
  FilterDataStatus onRequestBody(size_t body_buffer_length, bool end_of_stream) override;
  FilterHeadersStatus onResponseHeaders(uint32_t headers, bool end_of_stream) override;
  FilterDataStatus onResponseBody(size_t body_buffer_length, bool end_of_stream) override;
  void onDone() override;
  void onLog() override;
  void onDelete() override;

  opa_eval_ctx_t* eval_ctx_;
};

// static RegisterContextFactory register_ExampleContext(CONTEXT_FACTORY(ExampleContext),
//                                                       ROOT_FACTORY(ExampleRootContext),
//                                                       "opa_root_id");

// NOTE(sr): we're calling this to ensure that our implementation here is registered
// with the proxy_wasm_intrinsics.cc globals. The attempt above is suspect.
// This method is called by the module's Start function (_initialize), which is
// wired up in the wasm compiler.
extern "C" OPA_INTERNAL void _register_proxy_wasm(void) {
  RegisterContextFactory register_ExampleContext(CONTEXT_FACTORY(ExampleContext),
                                                 ROOT_FACTORY(ExampleRootContext),
                                                 "opa_root_id");
}

bool ExampleRootContext::onStart(size_t) {
  logInfo("onStart");
  return true;
}

bool ExampleRootContext::onConfigure(size_t) {
  logInfo("onConfigure");
  proxy_set_tick_period_milliseconds(1000); // 1 sec
  return true;
}

void ExampleRootContext::onTick() { logTrace("onTick"); }

void ExampleContext::onCreate() {
  logInfo("onCreate _");
  eval_ctx_ = opa_eval_ctx_new();
}

FilterHeadersStatus ExampleContext::onRequestHeaders(uint32_t, bool) {
  logInfo("onRequestHeaders _");
  auto result = getRequestHeaderPairs();
  auto pairs = result->pairs();
  logInfo("headers: _");
  opa_object_t *hdrs = opa_cast_object(opa_object());
  for (auto &p : pairs) {
    logInfo(std::string(p.first) + std::string(" -> ") + std::string(p.second));
    opa_object_insert(hdrs,
      opa_string_terminated(std::string(p.first).c_str()),
      opa_string_terminated(std::string(p.second).c_str()));
  }

  opa_object_t *input = opa_cast_object(opa_object());
  opa_object_insert(input, opa_string_terminated("headers"), &hdrs->hdr);
  opa_eval_ctx_set_input(eval_ctx_, &input->hdr);

  eval(eval_ctx_);
  opa_value *res = opa_eval_ctx_get_result(eval_ctx_);
  logInfo(opa_json_dump(res));
  return FilterHeadersStatus::Continue;
}

FilterHeadersStatus ExampleContext::onResponseHeaders(uint32_t, bool) {
  logDebug("onResponseHeaders _");
  return FilterHeadersStatus::Continue;
}

FilterDataStatus ExampleContext::onRequestBody(size_t body_buffer_length,
                                               bool /* end_of_stream */) {
  auto body = getBufferBytes(WasmBufferType::HttpRequestBody, 0, body_buffer_length);
  logError(std::string("onRequestBody ") + std::string(body->view()));
  return FilterDataStatus::Continue;
}

FilterDataStatus ExampleContext::onResponseBody(size_t /* body_buffer_length */,
                                                bool /* end_of_stream */) {
  setBuffer(WasmBufferType::HttpResponseBody, 0, 12, "Hello, world");
  return FilterDataStatus::Continue;
}

void ExampleContext::onDone() { logWarn(std::string("onDone _")); }

void ExampleContext::onLog() { logWarn(std::string("onLog _")); }

void ExampleContext::onDelete() { logWarn(std::string("onDelete _")); }
