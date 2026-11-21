package domain

import (
	"encoding/base64"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SecretKeys are settings keys that hold secrets or run credential
// helpers, at any depth. They never enter the Brain; they belong in a local
// overlay.
var SecretKeys = []string{"apiKeyHelper", "awsAuthRefresh", "awsCredentialExport", "env", "headers", "otelHeadersHelper"}

var (
	// A key named for a secret, such as apiKey or dbPassword.
	secretKeyPattern = regexp.MustCompile(`(?i)token|secret|passw|api[-_]?key|credential|private[-_]?key|access[-_]?key|session[-_]?key|cookie|authorization`)
	emailPattern     = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	// A home folder where a path starts, not a project folder named home.
	homePattern       = regexp.MustCompile(`(^|[\s"'(=:,/@])(/mnt/[a-z]|/var)?(/home/[^/\s"']+|/Users/[^/\s"']+|/root([/\s"')]|$))|[A-Za-z]:\\Users\\`)
	tokenPattern      = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{20,}|sk_(live|test)_[A-Za-z0-9]{10,}|hf_[A-Za-z0-9]{20,}|npm_[A-Za-z0-9]{20,}|ya29\.[A-Za-z0-9_-]{20,}|SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}|(?i:bearer)\s+[A-Za-z0-9._~+/-]{8,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+)`)
	credentialPattern = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----|[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`)
	basicPattern      = regexp.MustCompile(`(?i)\bbasic\s+([A-Za-z0-9+/]{4,}={0,2})`)
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
	if k := secretKeyIn(v); k != "" {
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

// secretKeyIn returns the first key that may hold secrets in the objects a
// value holds, at any depth and in lists too, or "".
func secretKeyIn(v any) string {
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if k := secretKeyIn(x); k != "" {
				return k
			}
		}
	case map[string]any:
		for _, k := range SortedKeys(t) {
			if secretKey([]string{k}) != "" {
				return k
			}
			if k := secretKeyIn(t[k]); k != "" {
				return k
			}
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
		case credentialPattern.MatchString(s) || basicCredential(s):
			why = "holds a credential"
		case emailPattern.MatchString(s):
			why = "holds an email address"
		case homes && homePattern.MatchString(s):
			why = "holds a home path: use {{brain}} or ~"
		}
	})
	return why
}

// basicCredential says whether s holds HTTP basic auth: the word, then
// user:password in base64, which a word after "basic" never decodes to.
func basicCredential(s string) bool {
	for _, m := range basicPattern.FindAllStringSubmatch(s, -1) {
		b, err := base64.StdEncoding.DecodeString(m[1])
		if err == nil && utf8.Valid(b) && strings.Contains(string(b), ":") && !strings.ContainsFunc(string(b), unicode.IsControl) {
			return true
		}
	}
	return false
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
		for _, k := range SortedKeys(t) {
			fn(k)
			walkStrings(t[k], fn)
		}
	}
}
