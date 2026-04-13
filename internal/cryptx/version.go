package cryptx

import (
	"path"
	"regexp"
	"strings"
	"time"
)

const versionFormat = "-v2006-01-02-150405.000"

var versionRegexp = regexp.MustCompile(`-v\d{4}-\d{2}-\d{2}-\d{6}-\d{3}`)

func splitExt(fileName string) (base, ext string) {
	ext = path.Ext(fileName)
	base = fileName[:len(fileName)-len(ext)]
	if ext != "" && base == "" {
		base, ext = ext, base
	}
	return base, ext
}

func addVersion(fileName string, t time.Time) string {
	base, ext := splitExt(fileName)
	s := strings.ReplaceAll(t.Format(versionFormat), ".", "-")
	return base + s + ext
}

func removeVersion(fileName string) (time.Time, string) {
	base, ext := splitExt(fileName)
	if len(base) < len(versionFormat) {
		return time.Time{}, fileName
	}
	versionStart := len(base) - len(versionFormat)
	if base[len(base)-4] != '-' {
		return time.Time{}, fileName
	}
	base = base[:len(base)-4] + "." + base[len(base)-3:]
	t, err := time.Parse(versionFormat, base[versionStart:])
	if err != nil {
		return time.Time{}, fileName
	}
	return t, base[:versionStart] + ext
}

func matchVersion(fileName string) bool {
	return versionRegexp.MatchString(fileName)
}
