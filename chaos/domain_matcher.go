package chaos

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

type domainMatcher struct {
	patterns []string
}

func newDomainMatcher(patterns []string) (*domainMatcher, error) {
	matcher := &domainMatcher{patterns: make([]string, 0, len(patterns))}
	for _, pattern := range patterns {
		normalizedPattern := normalizeDomain(pattern)
		if normalizedPattern == "" {
			return nil, errors.New("domain pattern must not be empty")
		}
		if starIndex := strings.IndexByte(normalizedPattern, '*'); starIndex >= 0 && starIndex != len(normalizedPattern)-1 {
			return nil, fmt.Errorf("wildcard must be the last character in domain pattern %q", pattern)
		}
		normalizedPattern = strings.ReplaceAll(normalizedPattern, "[!", "[^")
		if _, err := path.Match(normalizedPattern, ""); err != nil {
			return nil, fmt.Errorf("invalid domain pattern %q: %w", pattern, err)
		}
		matcher.patterns = append(matcher.patterns, normalizedPattern)
	}
	return matcher, nil
}

func (m *domainMatcher) Match(domain string) bool {
	normalizedDomain := normalizeDomain(domain)
	for _, pattern := range m.patterns {
		if matched, _ := path.Match(pattern, normalizedDomain); matched {
			return true
		}
	}
	return false
}

func normalizeDomain(domain string) string {
	return strings.TrimRight(strings.ToLower(domain), ".")
}
