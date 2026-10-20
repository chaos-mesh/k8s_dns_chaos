package chaos

import "testing"

func TestDomainMatcher(t *testing.T) {
	matcher, err := newDomainMatcher([]string{"google.com", "chaos-mesh.*", "github.?om", "chaos-test.local*"})
	if err != nil {
		t.Fatalf("newDomainMatcher() error = %v", err)
	}

	testCases := []struct {
		domain string
		want   bool
	}{
		{domain: "google.com", want: true},
		{domain: "GOOGLE.COM.", want: true},
		{domain: "chaos-mesh.org.", want: true},
		{domain: "github.com", want: true},
		{domain: "chaos-test.local.foo", want: true},
		{domain: "github.om", want: false},
		{domain: "github.company", want: false},
		{domain: "not-google.com", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.domain, func(t *testing.T) {
			if got := matcher.Match(testCase.domain); got != testCase.want {
				t.Fatalf("Match(%q) = %t, want %t", testCase.domain, got, testCase.want)
			}
		})
	}
}

func TestDomainMatcherRejectsInvalidPattern(t *testing.T) {
	for _, pattern := range []string{"", "*.example.com", "foo*bar", "foo**"} {
		t.Run(pattern, func(t *testing.T) {
			_, err := newDomainMatcher([]string{pattern})
			if err == nil {
				t.Fatalf("expected an error for domain pattern %q", pattern)
			}
		})
	}
}

func TestDomainMatcherLegacyCharacterClasses(t *testing.T) {
	for _, tt := range []struct {
		pattern, domain string
		want            bool
	}{
		{"db[12].example.com", "db1.example.com.", true},
		{"db[1-3].example.com", "db2.example.com", true},
		{"db[!9].example.com", "db2.example.com", true},
		{"db[!9].example.com", "db9.example.com", false},
	} {
		matcher, err := newDomainMatcher([]string{tt.pattern})
		if err != nil {
			t.Fatal(err)
		}
		if got := matcher.Match(tt.domain); got != tt.want {
			t.Fatalf("Match(%q, %q) = %t, want %t", tt.pattern, tt.domain, got, tt.want)
		}
	}
}
