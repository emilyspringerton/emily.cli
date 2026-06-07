// internal/color/color.go
// Terminal color helpers. Opt-in via EMILY_COLOR=1 env var.
// Zero-cost when disabled — all functions return the string unchanged.

package color

import "os"

var enabled = os.Getenv("EMILY_COLOR") == "1"

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	yellow = "\033[33m"
	green  = "\033[32m"
	cyan   = "\033[36m"
	bold   = "\033[1m"
)

// Severity colors the string by severity level (info/warn/error).
func Severity(s, level string) string {
	if !enabled {
		return s
	}
	switch level {
	case "error":
		return red + bold + s + reset
	case "warn":
		return yellow + s + reset
	case "info":
		return green + s + reset
	}
	return s
}

// Warn wraps s in yellow.
func Warn(s string) string {
	if !enabled {
		return s
	}
	return yellow + s + reset
}

// Bold wraps s in bold.
func Bold(s string) string {
	if !enabled {
		return s
	}
	return bold + s + reset
}

// Cyan wraps s in cyan.
func Cyan(s string) string {
	if !enabled {
		return s
	}
	return cyan + s + reset
}

// Enabled reports whether color output is active.
func Enabled() bool { return enabled }
