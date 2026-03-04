package framework

import (
	"os"
	"strconv"
	"strings"
)

// GetEnvInt returns the integer value of an environment variable,
// or the default value if not set or invalid.
func GetEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// GetEnvBool returns the boolean value of an environment variable,
// or the default value if not set.
// Accepts "true", "1", "yes" as true values (case-insensitive).
func GetEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		switch {
		case strings.EqualFold(val, "true"), val == "1", strings.EqualFold(val, "yes"):
			return true
		case strings.EqualFold(val, "false"), val == "0", strings.EqualFold(val, "no"):
			return false
		}
	}
	return defaultVal
}
