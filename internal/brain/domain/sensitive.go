package domain

import (
	"regexp"
	"slices"
	"strings"
)

// SecretKeys are settings keys that hold secrets or run credential
// helpers, at any depth. They never enter the Brain; they belong in a local
// overlay.
var SecretKeys = []string{"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "env", "headers", "otelHeadersHelper"}

var (
	// A key named for a secret, such as apiKey or dbPassword.
	secretKeyPattern = regexp.MustCompile(`(?i)token|secret|passw|api[-_]?key|credential`)
	emailPattern     = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	// A home folder where a path starts, not a project folder named home.
	homePattern       = regexp.MustCompile(`(^|[\s"'(=:,/])(/home/[^/\s"']+|/Users/[^/\s"']+|/root\b)|[A-Za-z]:\\Users\\`)
	tokenPattern      = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+)`)
	credentialPattern = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----|[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`)
)

// Sensitive says why a setting must stay out of the Brain, or "": it is
// under a key that holds secrets, or its key or value is identifying or
// secret. key is a leaf path.
func Sensitive(key string, v any) string { return sensitive(key, v, true) }

// secret says why a setting must not be shown, or "": as Sensitive, except
// for home paths, which this machine's reports show anyway.
func secret(key string, v any) string { return sensitive(key, v, false) }

func sensitive(key string, v any, homes bool) string {
	keys := strings.Split(key, Sep)
	if k := secretKey(keys); k != "" {
		return k + " may hold secrets"
	}
	if why := sensitiveText(keys, homes); why != "" {
		return "the key " + why
	}
	return sensitiveText(v, homes)
}

// secretKey returns the first key on a path that may hold secrets, or "".
func secretKey(path []string) string {
	for _, k := range path {
		if slices.Contains(SecretKeys, k) || secretKeyPattern.MatchString(k) {
			return k
		}
	}
	return ""
}

// SensitiveText says why a value is identifying or secret, or "".
func SensitiveText(v any) string { return sensitiveText(v, true) }

func sensitiveText(v any, homes bool) string {
	why := ""
	walkStrings(v, func(s string) {
		switch {
		case why != "":
		case tokenPattern.MatchString(s):
			why = "looks like a token"
		case credentialPattern.MatchString(s):
			why = "holds a credential"
		case emailPattern.MatchString(s):
			why = "holds an email address"
		case homes && homePattern.MatchString(s):
			why = "holds a home path: use {{brain}} or ~"
		}
	})
	return why
}

func walkStrings(v any, fn func(string)) {
	switch t := v.(type) {
	case string:
		fn(t)
	case []string:
		for _, x := range t {
			fn(x)
		}
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
