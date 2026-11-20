package domain

import (
	"regexp"
	"slices"
	"strings"
)

// SecretKeys are settings that hold secrets or run credential helpers. They
// never enter the Brain; they belong in a local overlay.
var SecretKeys = []string{"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "env", "otelHeadersHelper"}

var (
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	homePattern  = regexp.MustCompile(`/home/[^/\s"']+|/Users/[^/\s"']+|[A-Za-z]:\\Users\\`)
	tokenPattern = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16})`)
)

// Sensitive says why a setting must stay out of the Brain, or "". key is a
// leaf path.
func Sensitive(key string, v any) string {
	top, _, _ := strings.Cut(key, Sep)
	if slices.Contains(SecretKeys, top) {
		return top + " may hold secrets"
	}
	return SensitiveText(v)
}

// SensitiveText says why a value is identifying or secret, or "".
func SensitiveText(v any) string {
	why := ""
	walkStrings(v, func(s string) {
		switch {
		case why != "":
		case tokenPattern.MatchString(s):
			why = "looks like a token"
		case emailPattern.MatchString(s):
			why = "holds an email address"
		case homePattern.MatchString(s):
			why = "holds a home path: use {{brain}} or ~"
		}
	})
	return why
}

func walkStrings(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case []any:
		for _, x := range t {
			walkStrings(x, fn)
		}
	case map[string]any:
		for k, x := range t {
			fn(k)
			walkStrings(x, fn)
		}
	}
}
