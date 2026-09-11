package auth

// DevLoginEnabled is true only when local make targets set DEV_LOGIN=1.
// Cloud Run sets K_SERVICE, which forces this off even if the env var is
// injected by mistake.
func DevLoginEnabled(getenv func(string) string) bool {
	if getenv == nil {
		return false
	}
	return getenv("DEV_LOGIN") == "1" && getenv("K_SERVICE") == ""
}
