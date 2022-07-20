# GOOS=js GOARCH=wasm experiment

Rebuild `static/parser.wasm` via 

    GOOS=js GOARCH=wasm go build -o static/parser.wasm .

in this directory.

`wasm_exec.js` is from the golang root,

    cp "$(go env GOROOT)/misc/wasm/wasm_exec.js" static/
