package remoteauth

import (
	"regexp"
	"strings"
)

var wwwAuthParamRe = regexp.MustCompile(`([a-zA-Z_-]+)=(?:"([^"]*)"|([^\s,]+))`)

// ParseWWWAuthenticate extracts the resource_metadata URL (RFC 9728 §5.1) and
// scope from a WWW-Authenticate response header. Returns empty strings when
// absent.
func ParseWWWAuthenticate(header string) (resourceMetadataURL string, scope string) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", ""
	}
	for _, m := range wwwAuthParamRe.FindAllStringSubmatch(header, -1) {
		key := strings.ToLower(m[1])
		val := m[2]
		if val == "" {
			val = m[3]
		}
		switch key {
		case "resource_metadata":
			resourceMetadataURL = val
		case "scope":
			scope = val
		}
	}
	return resourceMetadataURL, scope
}
