package bundle

import (
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/internal/file/archive"
	"github.com/open-policy-agent/opa/v1/util"

	"github.com/open-policy-agent/opa/v1/util/test"
)

var benchTestArchiveFiles = map[string]string{
	"/a.json":                          `"a"`,
	"/a/b.json":                        `"b"`,
	"/a/b/c.json":                      `"c"`,
	"/a/b/d/data.json":                 `"hello"`,
	"/a/c/data.yaml":                   "12",
	"/some.txt":                        "text",
	"/policy.rego":                     "package foo\n p = 1",
	"/roles/policy.rego":               "package bar\n p = 1",
	"/deeper/dir/path/than/others/foo": "bar",
}

func BenchmarkTarballLoader(b *testing.B) {
	files := map[string]string{
		"/archive.tar.gz": "",
	}
	sizes := []int{1000, 10000, 100000, 250000}

	for _, n := range sizes {
		expectedFiles := make(map[string]string, len(benchTestArchiveFiles)+1)
		maps.Copy(expectedFiles, benchTestArchiveFiles)
		expectedFiles["/x/data.json"] = benchTestGetFlatDataJSON(n)

		// We generate the tarball once in the tempfs, and then reuse it many
		// times in the benchmark.
		test.WithTempFS(files, func(rootDir string) {
			tarballFile := filepath.Join(rootDir, "archive.tar.gz")
			benchTestCreateTarballFile(b, rootDir, expectedFiles)

			b.ResetTimer()

			f, err := os.Open(tarballFile)
			if err != nil {
				b.Fatalf("Unexpected error: %s", err)
			}
			defer f.Close()

			b.Run(strconv.Itoa(n), func(b *testing.B) {
				for b.Loop() {
					// Reset the file reader.
					if _, err := f.Seek(0, 0); err != nil {
						b.Fatalf("Unexpected error: %s", err)
					}
					loader := NewTarballLoaderWithBaseURL(f, tarballFile)
					benchTestLoader(b, loader)
				}
			})
		})
	}
}

func BenchmarkDirectoryLoader(b *testing.B) {
	for _, n := range []int{10000, 100000, 250000, 500000} {
		expectedFiles := make(map[string]string, len(benchTestArchiveFiles)+1)
		maps.Copy(expectedFiles, benchTestArchiveFiles)
		expectedFiles["/x/data.json"] = benchTestGetFlatDataJSON(n)

		test.WithTempFS(expectedFiles, func(rootDir string) {
			b.ResetTimer()
			b.Run(strconv.Itoa(n), func(b *testing.B) {
				for b.Loop() {
					benchTestLoader(b, NewDirectoryLoader(rootDir))
				}
			})
		})
	}
}

func BenchmarkDirectoryLoaderLargePolicy(b *testing.B) {
	for _, n := range []int{1000, 2500, 5000, 7500} {
		policyFiles := benchTestGenerateLargePolicyBundle(n)

		test.WithTempFS(policyFiles, func(rootDir string) {
			b.ResetTimer()
			b.Run(strconv.Itoa(n), func(b *testing.B) {
				for b.Loop() {
					benchTestLoader(b, NewDirectoryLoader(rootDir))
				}
			})
		})
	}
}

func BenchmarkDirectoryLoaderLargePolicyNoLocation(b *testing.B) {
	for _, n := range []int{1000, 2500, 5000, 7500} {
		policyFiles := benchTestGenerateLargePolicyBundle(n)

		test.WithTempFS(policyFiles, func(rootDir string) {
			b.ResetTimer()
			b.Run(strconv.Itoa(n), func(b *testing.B) {
				for b.Loop() {
					benchTestLoaderNoLocation(b, NewDirectoryLoader(rootDir))
				}
			})
		})
	}
}

// Creates a flat JSON object of configurable size.
func benchTestGetFlatDataJSON(numKeys int) string {
	largeFile := make(map[string]string, numKeys)
	for i := range numKeys {
		largeFile[strconv.FormatInt(int64(i), 10)] = strings.Repeat("A", 1024)
	}
	return string(util.MustMarshalJSON(largeFile))
}

// Generates a tarball with a data.json of variable size.
func benchTestCreateTarballFile(b *testing.B, root string, filesToWrite map[string]string) {
	b.Helper()

	tarballFile := filepath.Join(root, "archive.tar.gz")
	f, err := os.Create(tarballFile)
	if err != nil {
		b.Fatalf("Unexpected error: %s", err)
	}

	gzFiles := make([][2]string, 0, len(filesToWrite))
	for name, content := range filesToWrite {
		gzFiles = append(gzFiles, [2]string{name, content})
	}

	_, err = f.Write(archive.MustWriteTarGz(gzFiles).Bytes())
	if err != nil {
		b.Fatalf("Unexpected error: %s", err)
	}
	f.Close()
}

func benchTestLoader(b *testing.B, loader DirectoryLoader) {
	b.Helper()

	br := NewCustomReader(loader).WithLazyLoadingMode(true)
	bundle, err := br.Read()
	if err != nil {
		b.Fatal(err)
	}

	if len(bundle.Raw) == 0 {
		b.Fatal("bundle.Raw is unexpectedly empty")
	}
}

func benchTestLoaderNoLocation(b *testing.B, loader DirectoryLoader) {
	b.Helper()

	br := NewCustomReader(loader).WithLazyLoadingMode(true).WithSkipLocationMetadata(true)
	bundle, err := br.Read()
	if err != nil {
		b.Fatal(err)
	}

	if len(bundle.Raw) == 0 {
		b.Fatal("bundle.Raw is unexpectedly empty")
	}
}

func benchTestGenerateLargePolicyBundle(numRules int) map[string]string {
	var policy strings.Builder
	policy.Grow((140 * numRules) + 100)

	policy.WriteString("package example.large.partial.rules.policy\n\n")
	for i := range numRules {
		policy.WriteString(benchTestGeneratePolicyRule(i))
		policy.WriteString("\n\n")
	}
	policy.WriteString("number_denies = x if {\n\tx := count(deny)\n}")

	return map[string]string{
		"/policy.rego": policy.String(),
	}
}

func benchTestGeneratePolicyRule(n int) string {
	return strings.Join([]string{
		"deny contains [resource, errormsg] if {",
		"\tresource := \"example." + strconv.Itoa(n) + "\"",
		"\ti := " + strconv.Itoa(n),
		"\ti % 2 != 0",
		"\terrormsg := \"denied because " + strconv.Itoa(n) + " is an odd number.\"",
		"}",
	}, "\n")
}
