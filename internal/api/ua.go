package api

import "strings"

// uaSummary extracts a coarse browser and OS from a User-Agent for the session list. It is a display aid only —
// never used for security decisions.
func uaSummary(ua string) (browser, os string) {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "edg/"):
		browser = "Edge"
	case strings.Contains(l, "firefox/"):
		browser = "Firefox"
	case strings.Contains(l, "chrome/") || strings.Contains(l, "crios/"):
		browser = "Chrome"
	case strings.Contains(l, "safari/"):
		browser = "Safari"
	case strings.Contains(l, "curl/"):
		browser = "curl"
	default:
		browser = "Unknown browser"
	}
	switch {
	case strings.Contains(l, "android"):
		os = "Android"
	case strings.Contains(l, "iphone") || strings.Contains(l, "ipad"):
		os = "iOS"
	case strings.Contains(l, "windows"):
		os = "Windows"
	case strings.Contains(l, "mac os x") || strings.Contains(l, "macintosh"):
		os = "macOS"
	case strings.Contains(l, "linux") || strings.Contains(l, "x11"):
		os = "Linux"
	default:
		os = "Unknown OS"
	}
	return
}
