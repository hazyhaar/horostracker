package e2e

import (
	"os"
	"sync"
	"testing"
)

// harness and dba are global state shared by all tests.
// They are initialized once by the first test that calls ensureHarness.
var (
	harness     *TestHarness
	dba         *DBAssert
	harnessOnce sync.Once
)

// ensureHarness starts the harness on first call. All subsequent calls
// return the existing harness. The first caller's testing.T is used
// for server lifecycle management.
func ensureHarness(t *testing.T) (*TestHarness, *DBAssert) {
	t.Helper()
	harnessOnce.Do(func() {
		harness = NewHarness(t)
		dba = NewDBAssert(harness.NodesDB, harness.FlowsDB, harness.MetricsDB)

		wd, _ := os.Getwd()
		InitGlobalResults(wd)
	})

	if harness == nil {
		t.Fatal("harness initialization failed")
	}
	return harness, dba
}

// TestMain configures the test binary and ensures proper cleanup.
func TestMain(m *testing.M) {
	exitCode := m.Run()

	// Cleanup after all tests
	if dba != nil {
		dba.Close()
	}
	if harness != nil {
		harness.Stop()
	}
	CloseGlobalResults()

	os.Exit(exitCode)
}
