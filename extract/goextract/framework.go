package goextract

import "strings"

// FrameworkWebPackagePath is the canonical example path used in tests and
// pre-registered response types. Runtime extraction treats any import path
// ending in "/web" as a web framework helper package.
const FrameworkWebPackagePath = "example.com/webframework/web"

func isFrameworkWebPackage(path string) bool {
	return strings.HasSuffix(path, "/web")
}
