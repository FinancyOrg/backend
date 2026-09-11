package auth

import (
	"fmt"
	"sort"
	"strings"
)

// Allowlist answers whether an email address may sign in.
type Allowlist struct {
	emails map[string]struct{}
}

// NewAllowlist builds an Allowlist from explicit emails. Matching is
// case-insensitive and surrounding whitespace is ignored.
func NewAllowlist(emails []string) *Allowlist {
	a := &Allowlist{emails: make(map[string]struct{}, len(emails))}
	for _, e := range emails {
		if e = normalizeEmail(e); e != "" {
			a.emails[e] = struct{}{}
		}
	}
	return a
}

// LoadAllowlist resolves the allowlist from the ALLOWED_USERS environment
// variable: a comma-separated list of email addresses. The variable is
// required (deployed services get it from terraform) — a missing or empty
// value is fatal at startup rather than silently allowing nobody.
func LoadAllowlist(getenv func(string) string) (*Allowlist, error) {
	raw := strings.TrimSpace(getenv("ALLOWED_USERS"))
	if raw == "" {
		return nil, fmt.Errorf("ALLOWED_USERS is required (comma-separated email addresses)")
	}
	return ParseAllowlist([]byte(raw))
}

// ParseAllowlist parses a comma-separated list of email addresses. Newlines
// are treated as commas for convenience. Every entry must look like an email
// address — malformed entries are fatal at startup, not silently ignored.
func ParseAllowlist(raw []byte) (*Allowlist, error) {
	fields := strings.FieldsFunc(string(raw), func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	for _, f := range fields {
		if !strings.Contains(f, "@") {
			return nil, fmt.Errorf("parse allowlist: %q is not an email address", f)
		}
	}
	return NewAllowlist(fields), nil
}

// Allowed reports whether email may sign in.
func (a *Allowlist) Allowed(email string) bool {
	_, ok := a.emails[normalizeEmail(email)]
	return ok
}

// Primary returns a stable allowlisted email, or "" if none. Used by the
// local-only DEV_LOGIN path so the UI can start without Google.
func (a *Allowlist) Primary() string {
	if a == nil || len(a.emails) == 0 {
		return ""
	}
	emails := make([]string, 0, len(a.emails))
	for e := range a.emails {
		emails = append(emails, e)
	}
	sort.Strings(emails)
	return emails[0]
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
