package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// String is the trimmed contents of VERSION. The image pipeline uses the same
// file as the container tag, so health reports what is running.
func String() string {
	return strings.TrimSpace(raw)
}
