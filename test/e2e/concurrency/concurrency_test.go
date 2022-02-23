package metrics

import (
	"flag"
	"io/ioutil"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/open-policy-agent/opa/storage/disk"
	"github.com/open-policy-agent/opa/test/e2e"
)

var testRuntime *e2e.TestRuntime

func TestMain(m *testing.M) {
	flag.Parse()
	testServerParams := e2e.NewAPIServerTestParams()

	dir, err := ioutil.TempDir("", "disk-store")
	if err != nil {
		panic(err)
	}

	for _, opts := range []*disk.Options{
		nil,
		{Dir: dir, Partitions: nil},
	} {
		var err error
		testServerParams.DiskStorage = opts
		testRuntime, err = e2e.NewTestRuntime(testServerParams)
		if err != nil {
			panic(err)
		}
		if ec := testRuntime.RunTests(m); ec != 0 {
			os.Exit(ec)
		}
	}
}

func TestConcurrency(t *testing.T) {

	policy := `
	package test
	p = true
	`

	err := testRuntime.UploadPolicy(t.Name(), strings.NewReader(policy))
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	n := runtime.NumCPU()
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				dr := struct {
					Result bool `json:"result"`
				}{}
				if err := testRuntime.GetDataWithInputTyped("test/p", nil, &dr); err != nil {
					t.Error(err)
					return
				}
				if !dr.Result {
					t.Errorf("Unexpected response: %+v", dr)
					return
				}
			}
		}()
	}

	wg.Wait()
}
