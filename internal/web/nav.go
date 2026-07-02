package web

import "strings"

// navIsActive reports whether the request path belongs to the nav item whose
// base path is base. The dashboard ("/") matches only exactly; every other
// base matches its exact path or a sub-path ("/hosts" matches "/hosts" and
// "/hosts/5" but not "/hostsx"), so shared prefixes don't false-positive.
func navIsActive(path, base string) bool {
	if base == "/" {
		return path == "/"
	}
	return path == base || strings.HasPrefix(path, base+"/")
}
