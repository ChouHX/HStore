package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hstore-db-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("FAKA_DB_PATH", filepath.Join(dir, "test.db")); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
