// Copyright 2022 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package disk_test

import (
	"errors"
	"io/ioutil"
	"os"
	"testing"

	"github.com/open-policy-agent/opa/storage/disk"
)

func TestNewFromConfig(t *testing.T) {
	tmpdir, err := ioutil.TempDir("", "disk_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpdir) })

	for _, tc := range []struct {
		note    string
		config  string
		err     error // gets unwrapped
		nothing bool  // returns no disk store?
	}{
		{
			note:    "no storage section",
			config:  "",
			nothing: true,
		},
		{
			note: "successful init, no partitions",
			config: `
storage:
  directory: "` + tmpdir + `"
`,
		},
		{
			note: "successful init, valid partitions",
			config: `
storage:
  directory: "` + tmpdir + `"
  partitions:
  - /foo/bar
  - /baz
`,
		},
		{
			note: "partitions invalid",
			config: `
storage:
  directory: "` + tmpdir + `"
  partitions:
  - /foo/bar
  - baz
`,
			err: disk.ErrInvalidPartitionPath,
		},
		{
			note: "directory does not exist",
			config: `
storage:
  directory: "` + tmpdir + `/foobar"
`,
			err: os.ErrNotExist,
		},
	} {
		t.Run(tc.note, func(t *testing.T) {
			d, err := disk.OptionsFromConfig([]byte(tc.config), "id")
			if !errors.Is(err, tc.err) {
				t.Errorf("err: expected %v, got %v", tc.err, err)
			}
			if tc.nothing && d != nil {
				t.Errorf("expected no disk options, got %v", d)
			}
		})
	}
}
