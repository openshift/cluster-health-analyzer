package framework

import (
	"os"
	"strconv"
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
		switch val {
		case "true", "TRUE", "True", "1", "yes", "YES", "Yes":
			return true
		case "false", "FALSE", "False", "0", "no", "NO", "No":
			return false
		}
	}
	return defaultVal
}
