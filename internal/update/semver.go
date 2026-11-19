package update

import (
	"regexp"
	"strconv"
	"strings"
)

var semverRe = regexp.MustCompile(`^(\d{1,9})\.(\d{1,9})\.(\d{1,9})(-[0-9A-Za-z.-]{1,40})?$`)

// ValidVersion reports whether v (without 'v') is major.minor.patch with an optional -suffix.
func ValidVersion(v string) bool { return semverRe.MatchString(v) }

// Compare compares two versions without the leading 'v': -1, 0 or 1.
// Any '-' suffix sorts below the same version without one; two suffixes compare by SemVer rules.
// Unparsable versions sort below everything.
func Compare(a, b string) int {
	ma, mb := semverRe.FindStringSubmatch(strings.TrimPrefix(a, "v")), semverRe.FindStringSubmatch(strings.TrimPrefix(b, "v"))
	switch {
	case ma == nil && mb == nil:
		return 0
	case ma == nil:
		return -1
	case mb == nil:
		return 1
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	pa, pb := ma[4], mb[4]
	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	}
	return comparePre(pa[1:], pb[1:])
}

// comparePre orders two prerelease suffixes by SemVer rules: dot-separated identifiers,
// numeric ones compared as numbers and below alphanumeric ones, a shorter list below a longer one.
func comparePre(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, y := pa[i], pb[i]
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
		case ex == nil:
			return -1
		case ey == nil:
			return 1
		case x != y:
			if x < y {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(pa) < len(pb):
		return -1
	case len(pa) > len(pb):
		return 1
	}
	return 0
}
