package bundle

import (
	"fmt"
	"strings"
	"testing"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/util/test"
)

func TestSkipLocationMetadataInBundle(t *testing.T) {
	policy := `package example

deny contains [resource, errormsg] if {
	resource := "example.1"
	i := 1
	i % 2 != 0
	errormsg := "denied because 1 is an odd number."
}

deny contains [resource, errormsg] if {
	resource := "example.2"
	i := 2
	i % 2 != 0
	errormsg := "denied because 2 is an odd number."
}`

	files := map[string]string{
		"/policy.rego": policy,
	}

	t.Run("WithLocationMetadata", func(t *testing.T) {
		test.WithTempFS(files, func(rootDir string) {
			br := NewCustomReader(NewDirectoryLoader(rootDir)).WithLazyLoadingMode(true)
			bundle, err := br.Read()
			if err != nil {
				t.Fatal(err)
			}

			if len(bundle.Modules) == 0 {
				t.Fatal("expected modules in bundle")
			}

			locationCount := 0
			for _, mf := range bundle.Modules {
				locationCount += countLocations(mf.Parsed)
			}

			if locationCount == 0 {
				t.Error("expected locations to be set, but found none")
			}
			t.Logf("Found %d non-nil locations WITH metadata", locationCount)
		})
	})

	t.Run("WithoutLocationMetadata", func(t *testing.T) {
		test.WithTempFS(files, func(rootDir string) {
			br := NewCustomReader(NewDirectoryLoader(rootDir)).
				WithLazyLoadingMode(true).
				WithSkipLocationMetadata(true)
			bundle, err := br.Read()
			if err != nil {
				t.Fatal(err)
			}

			if len(bundle.Modules) == 0 {
				t.Fatal("expected modules in bundle")
			}

			locationCount := 0
			var allDetails []string
			packageAndRuleCount := 0
			for _, mf := range bundle.Modules {
				count, details := countLocationsWithDetails(mf.Parsed)
				locationCount += count
				allDetails = append(allDetails, details...)

				// Count Package and Rule locations (which are always set for annotation processing)
				for _, detail := range details {
					if strings.Contains(detail, "Package at") || strings.Contains(detail, "Rule at") {
						packageAndRuleCount++
					}
				}
			}

			// Package and Rule locations are always set (needed for annotation processing during compilation)
			// All other locations should be nil when SkipLocationMetadata is enabled
			nonEssentialLocations := locationCount - packageAndRuleCount
			if nonEssentialLocations > 0 {
				t.Errorf("expected only Package/Rule locations, but found %d other locations", nonEssentialLocations)
				for _, detail := range allDetails {
					if !strings.Contains(detail, "Package at") && !strings.Contains(detail, "Rule at") {
						t.Logf("  - %s", detail)
					}
				}
			}
			t.Logf("Found %d locations (%d Package/Rule, %d others) WITHOUT metadata",
				locationCount, packageAndRuleCount, nonEssentialLocations)
		})
	})
}

type locationCounter struct {
	count   int
	details []string
}

func (lc *locationCounter) Visit(x interface{}) ast.Visitor {
	switch n := x.(type) {
	case *ast.Package:
		if n.Location != nil {
			lc.count++
			lc.details = append(lc.details, fmt.Sprintf("Package at %v", n.Location))
		}
	case *ast.Import:
		if n.Location != nil {
			lc.count++
			lc.details = append(lc.details, fmt.Sprintf("Import at %v", n.Location))
		}
	case *ast.Rule:
		if n.Location != nil {
			lc.count++
			lc.details = append(lc.details, fmt.Sprintf("Rule at %v", n.Location))
		}
	case *ast.Head:
		if n.Location != nil {
			lc.count++
			lc.details = append(lc.details, fmt.Sprintf("Head at %v", n.Location))
		}
	case *ast.Expr:
		if n.Location != nil {
			lc.count++
			lc.details = append(lc.details, fmt.Sprintf("Expr at %v", n.Location))
		}
	case *ast.Term:
		if n.Location != nil {
			lc.count++
			lc.details = append(lc.details, fmt.Sprintf("Term at %v", n.Location))
		}
	}
	return lc
}

func countLocations(module *ast.Module) int {
	lc := &locationCounter{}
	ast.Walk(lc, module)
	return lc.count
}

func countLocationsWithDetails(module *ast.Module) (int, []string) {
	lc := &locationCounter{}
	ast.Walk(lc, module)
	return lc.count, lc.details
}

func TestLargeBundleLocationCheck(t *testing.T) {
	numRules := 100

	var policy strings.Builder
	policy.WriteString("package example\n\n")
	for i := range numRules {
		policy.WriteString(fmt.Sprintf(`deny contains [resource, errormsg] if {
	resource := "example.%d"
	i := %d
	i %% 2 != 0
	errormsg := "denied because %d is an odd number."
}

`, i, i, i))
	}

	files := map[string]string{
		"/policy.rego": policy.String(),
	}

	test.WithTempFS(files, func(rootDir string) {
		t.Run("WithLocationMetadata", func(t *testing.T) {
			br := NewCustomReader(NewDirectoryLoader(rootDir)).WithLazyLoadingMode(true)
			bundle, err := br.Read()
			if err != nil {
				t.Fatal(err)
			}

			locationCount := 0
			for _, mf := range bundle.Modules {
				locationCount += countLocations(mf.Parsed)
			}
			t.Logf("WITH metadata: %d locations", locationCount)
		})

		t.Run("WithoutLocationMetadata", func(t *testing.T) {
			br := NewCustomReader(NewDirectoryLoader(rootDir)).
				WithLazyLoadingMode(true).
				WithSkipLocationMetadata(true)
			bundle, err := br.Read()
			if err != nil {
				t.Fatal(err)
			}

			locationCount := 0
			packageAndRuleCount := 0
			for _, mf := range bundle.Modules {
				count, details := countLocationsWithDetails(mf.Parsed)
				locationCount += count

				// Count Package and Rule locations (which are always set)
				for _, detail := range details {
					if strings.Contains(detail, "Package at") || strings.Contains(detail, "Rule at") {
						packageAndRuleCount++
					}
				}
			}
			t.Logf("WITHOUT metadata: %d locations (%d Package/Rule)", locationCount, packageAndRuleCount)

			// Package and Rule locations are always set (needed for annotation processing during compilation)
			// All other locations should be nil when SkipLocationMetadata is enabled
			if locationCount != packageAndRuleCount {
				t.Errorf("expected only Package/Rule locations, but found %d total locations (%d Package/Rule, %d others)",
					locationCount, packageAndRuleCount, locationCount-packageAndRuleCount)
			}
		})
	})
}
