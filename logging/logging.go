package logging

import (
	"fmt"
	"os"
)

// VerboseMode controls logging output - will be set by main package
var VerboseMode bool

// LogDebug prints debug messages only in verbose mode
func LogDebug(format string, args ...interface{}) {
	if VerboseMode {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// LogInfo prints info messages only in verbose mode
func LogInfo(format string, args ...interface{}) {
	if VerboseMode {
		fmt.Printf("[INFO] "+format+"\n", args...)
	}
}

// LogError prints error messages (always shown)
func LogError(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[ERROR] "+format+"\n", args...)
}

// SetVerboseMode sets the global verbose logging mode
func SetVerboseMode(verbose bool) {
	VerboseMode = verbose
}