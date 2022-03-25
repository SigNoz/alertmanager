package constants

import (
	"os"
	"strconv"
)

// GetOrDefaultEnv looks for environment variable if set, or
// returns a fallback value passed by caller
func GetOrDefaultEnv(key string, fallback string) string {
	v := os.Getenv(key)
	if len(v) == 0 {
		return fallback
	}
	return v
}

// GetOrDefaultEnvInt looks for environment variable if set, or
// returns a fallback value passed by caller
func GetOrDefaultEnvInt(key string, fallback int) (int) {
	v := os.Getenv(key)

	if len(v) == 0 {
		return fallback
	}
	i, err := strconv.Atoi(v) 

	if err != nil {
		return fallback
	}

	return i
}