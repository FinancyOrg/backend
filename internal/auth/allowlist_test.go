package auth

import "testing"

func TestLoadAllowlistFromEnv(t *testing.T) {
	a, err := LoadAllowlist(func(string) string { return "alice@example.com, bob@example.com" })
	if err != nil {
		t.Fatal(err)
	}
	if !a.Allowed("alice@example.com") || !a.Allowed("bob@example.com") {
		t.Fatal("env emails should be allowed")
	}
	if a.Allowed("mallory@example.com") {
		t.Fatal("unlisted email must not be allowed")
	}
}

func TestLoadAllowlistRequiresEnv(t *testing.T) {
	for _, v := range []string{"", "  \n "} {
		if _, err := LoadAllowlist(func(string) string { return v }); err == nil {
			t.Fatalf("ALLOWED_USERS=%q must be fatal, not silently empty", v)
		}
	}
}

func TestParseAllowlistCSV(t *testing.T) {
	a, err := ParseAllowlist([]byte("a@example.com, b@example.com\n,c@example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		if !a.Allowed(email) {
			t.Fatalf("%q should be allowed", email)
		}
	}
	if a.Allowed("d@example.com") {
		t.Fatal("unlisted email must not be allowed")
	}
}

func TestParseAllowlistRejectsMalformedEntries(t *testing.T) {
	for _, raw := range []string{"not-an-email", "ok@example.com,bad-entry", "no-at-sign\n"} {
		if _, err := ParseAllowlist([]byte(raw)); err == nil {
			t.Fatalf("%q must fail", raw)
		}
	}
}

func TestAllowlistMatching(t *testing.T) {
	a := NewAllowlist([]string{"Alice@Example.com", "", "  "})
	cases := []struct {
		email string
		want  bool
	}{
		{"alice@example.com", true},   // case-insensitive
		{" Alice@Example.com ", true}, // whitespace ignored
		{"bob@example.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := a.Allowed(c.email); got != c.want {
			t.Errorf("Allowed(%q) = %v, want %v", c.email, got, c.want)
		}
	}
}

func TestAllowlistFailsClosed(t *testing.T) {
	if NewAllowlist(nil).Allowed("anyone@example.com") {
		t.Fatal("empty allowlist must allow nobody")
	}
}

func TestAllowlistPrimary(t *testing.T) {
	if NewAllowlist(nil).Primary() != "" {
		t.Fatal("empty allowlist has no primary")
	}
	a := NewAllowlist([]string{"bob@example.com", "alice@example.com"})
	if got := a.Primary(); got != "alice@example.com" {
		t.Fatalf("Primary() = %q, want alice@example.com (sorted)", got)
	}
}

func TestDevLoginEnabled(t *testing.T) {
	env := func(dev, kService string) func(string) string {
		return func(k string) string {
			switch k {
			case "DEV_LOGIN":
				return dev
			case "K_SERVICE":
				return kService
			default:
				return ""
			}
		}
	}
	if DevLoginEnabled(nil) {
		t.Fatal("nil getenv must be off")
	}
	if !DevLoginEnabled(env("1", "")) {
		t.Fatal("DEV_LOGIN=1 with no K_SERVICE must be on")
	}
	if DevLoginEnabled(env("1", "financy-backend")) {
		t.Fatal("Cloud Run must never enable DEV_LOGIN")
	}
	if DevLoginEnabled(env("", "")) {
		t.Fatal("unset DEV_LOGIN must be off")
	}
}
