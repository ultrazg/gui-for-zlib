package core

import (
	"cmp"
	"fmt"
	"regexp"
	"strings"
)

// Accept release tags with an optional v prefix. Build metadata does not
// affect precedence; numeric parts are compared without integer overflow.
var releaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

type releaseVersion struct {
	parts []string
	pre   []string
}

func parseReleaseVersion(raw string) (releaseVersion, error) {
	m := releaseVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return releaseVersion{}, fmt.Errorf("无法识别版本号 %q，应为 vX.Y.Z", raw)
	}
	v := releaseVersion{parts: m[1:4]}
	if m[4] != "" {
		v.pre = strings.Split(m[4], ".")
		for _, p := range v.pre {
			if numericVersionPart(p) && len(p) > 1 && p[0] == '0' {
				return releaseVersion{}, fmt.Errorf("无法识别版本号 %q：预发布数字不能以 0 开头", raw)
			}
		}
	}
	return v, nil
}

func numericVersionPart(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func compareNumericVersionPart(a, b string) int {
	if n := cmp.Compare(len(a), len(b)); n != 0 {
		return n
	}
	return strings.Compare(a, b)
}

func (v releaseVersion) compare(other releaseVersion) int {
	for i, part := range v.parts {
		if n := compareNumericVersionPart(part, other.parts[i]); n != 0 {
			return n
		}
	}
	if len(v.pre) == 0 && len(other.pre) == 0 {
		return 0
	}
	if len(v.pre) == 0 {
		return 1
	}
	if len(other.pre) == 0 {
		return -1
	}
	for i := 0; i < min(len(v.pre), len(other.pre)); i++ {
		a, b := v.pre[i], other.pre[i]
		an, bn := numericVersionPart(a), numericVersionPart(b)
		n := strings.Compare(a, b)
		switch {
		case an && bn:
			n = compareNumericVersionPart(a, b)
		case an:
			n = -1
		case bn:
			n = 1
		}
		if n != 0 {
			return n
		}
	}
	return cmp.Compare(len(v.pre), len(other.pre))
}
