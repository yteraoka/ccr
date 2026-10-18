package ccr

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps the tests from reading the developer's own ccr config
// file, which would add their real config directories to every lookup.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ccr-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("CCR_CONFIG", filepath.Join(dir, "no-such-config.json")) //nolint:errcheck
	code := m.Run()
	os.RemoveAll(dir) //nolint:errcheck
	os.Exit(code)
}
