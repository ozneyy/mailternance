package storage

import (
	"os"
	"path/filepath"
)

var DataDir = getEnvOr("DATA_DIR", ".")

// GetStoragePath returns the absolute or relative path for a file inside the configured DataDir.
// It also ensures the DataDir directory exists.
func GetStoragePath(filename string) string {
	if DataDir != "." {
		_ = os.MkdirAll(DataDir, 0755)
	}
	return filepath.Join(DataDir, filename)
}

func getEnvOr(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}
