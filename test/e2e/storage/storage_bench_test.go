// Copyright 2022 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.
package storage

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/logging"
	"github.com/open-policy-agent/opa/storage/disk"
	"github.com/open-policy-agent/opa/test/e2e"
)

func diskStorage(tb testing.TB) *disk.Options {
	tb.Helper()

	dir, err := ioutil.TempDir("", "disk-store")
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = os.RemoveAll(dir) })

	return &disk.Options{Dir: dir, Partitions: nil}
}

func runtime(tb testing.TB, disk bool) *e2e.TestRuntime {
	tb.Helper()
	params := e2e.NewAPIServerTestParams()
	if disk {
		params.DiskStorage = diskStorage(tb)
	}
	tr, err := e2e.NewTestRuntime(params)
	if err != nil {
		tb.Fatal(err)
	}
	return tr
}

func TestStorageOneBlob(t *testing.T) {
	dataOfSize := func(n int) string {
		buf := strings.Builder{}
		buf.WriteString(`{"array": [`)
		for i := 0; i < n; i++ {
			if i != 0 {
				buf.WriteRune(',')
			}
			fmt.Fprintf(&buf, `{"user":"uid-%d","group":"gid-%d"}`, i, i)
		}
		buf.WriteString(`]}`)
		return buf.String()
	}

	t.Run("store=inmem", func(t *testing.T) {
		rt := func() *e2e.TestRuntime { return runtime(t, false) }
		for _, n := range []int{10, 100, 10e2, 10e3, 10e4, 10e5, 10e6} {
			t.Run(fmt.Sprintf("n=%d", n), runTest(t, rt, dataOfSize(n)))
		}
	})

	t.Run("store=disk", func(t *testing.T) {
		rt := func() *e2e.TestRuntime { return runtime(t, true) }
		for _, n := range []int{10, 100, 10e2, 10e3, 10e4, 10e5, 10e6} {
			t.Run(fmt.Sprintf("n=%d", n), runTest(t, rt, dataOfSize(n)))
		}
	})
}

func runTest(t *testing.T, rt func() *e2e.TestRuntime, data string) func(*testing.T) {
	return func(t *testing.T) {
		rt, _ := start(t, rt())

		if err := rt.UploadData(strings.NewReader(data)); err != nil {
			t.Fatal(err)
		}

		// shutdown
		// rt.Cancel()
		// if err := <-done; err != nil {
		// t.Fatal(err)
		// }
	}
}

func start(tb testing.TB, rt *e2e.TestRuntime) (*e2e.TestRuntime, chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		// Suppress the stdlogger in the server
		if !testing.Verbose() {
			logging.Get().SetOutput(ioutil.Discard)
		}
		err := rt.Runtime.Serve(ctx)
		done <- err
	}()
	if err := rt.WaitForServer(); err != nil {
		tb.Fatal(err)
	}
	return e2e.WrapRuntime(ctx, cancel, rt.Runtime), done
}
