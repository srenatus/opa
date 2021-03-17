#!/bin/sh

set -e

GOFLAGS=-mod=vendor GO111MODULE=on CGO_ENABLED=0 WASM_ENABLED=0 GOOS="" GOARCH="" go run -tags generate $@
