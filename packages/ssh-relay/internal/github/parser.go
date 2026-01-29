package github

import (
	"regexp"
	"strings"
)

var (
	// Patterns for parsing GitHub URLs
	httpsPattern = regexp.MustCompile(`^https?://github\.com/([^/]+/[^/]+?)(?:\.git)?/?$`)
	sshPattern   = regexp.MustCompile(`^git@github\.com:([^/]+/[^/]+?)(?:\.git)?$`)
	shortPattern = regexp.MustCompile(`^github\.com/([^/]+/[^/]+?)(?:\.git)?/?$`)
	barePattern  = regexp.MustCompile(`^([a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+)$`)
)

// ParseRepo extracts a GitHub repo identifier from various URL formats
// Returns empty string if input is not a valid GitHub reference
//
// Supported formats:
//   - https://github.com/user/repo
//   - https://github.com/user/repo.git
//   - github.com/user/repo
//   - git@github.com:user/repo.git
//   - user/repo
func ParseRepo(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	// Try HTTPS URL
	if matches := httpsPattern.FindStringSubmatch(input); len(matches) > 1 {
		return matches[1]
	}

	// Try SSH URL
	if matches := sshPattern.FindStringSubmatch(input); len(matches) > 1 {
		return matches[1]
	}

	// Try short format (github.com/user/repo)
	if matches := shortPattern.FindStringSubmatch(input); len(matches) > 1 {
		return matches[1]
	}

	// Try bare format (user/repo)
	if matches := barePattern.FindStringSubmatch(input); len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// IsValidRepo checks if a string looks like a valid GitHub repo reference
func IsValidRepo(input string) bool {
	return ParseRepo(input) != ""
}
