package sourcemap

import (
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/sourceloc"
)

func GitHubURL(loc sourceloc.Location, repoSlug, branch string) string {
	repoSlug = strings.TrimSpace(repoSlug)
	if repoSlug == "" || loc.File == "" {
		return ""
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}

	cleanPath := strings.TrimPrefix(path.Clean(strings.ReplaceAll(loc.File, "\\", "/")), "/")
	escapedPath := url.PathEscape(cleanPath)
	escapedBranch := url.PathEscape(branch)
	u := "https://github.com/" + repoSlug + "/blob/" + escapedBranch + "/" + escapedPath
	if loc.Line > 0 {
		u += "#L" + strconv.Itoa(loc.Line)
	}
	return u
}
