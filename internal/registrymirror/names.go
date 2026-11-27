package registrymirror

import (
	"regexp"
	"strings"
)

// The registry's own grammar for what a request may name. A job chooses the
// path and the mirror makes the outbound request for it, so anything outside
// these is refused before a request is made upstream.
var (
	// A repository name is path components joined by slashes, each lower-case
	// alphanumerics with single separators between them, at most 255 bytes.
	nameRE = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*$`)
	tagRE  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	// Only sha256 is served. A digest in any other algorithm is a reference
	// the mirror cannot verify, so it is refused rather than passed on.
	digestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

const maxNameLength = 255

func validName(name string) bool {
	return len(name) <= maxNameLength && nameRE.MatchString(name)
}

func validTag(tag string) bool { return tagRE.MatchString(tag) }

func validDigest(digest string) bool { return digestRE.MatchString(digest) }

// isDigest says whether a manifest reference names content rather than a tag.
// A tag cannot contain a colon, so anything that does is read as a digest and
// held to the digest grammar, whatever algorithm it names.
func isDigest(ref string) bool { return strings.Contains(ref, ":") }
