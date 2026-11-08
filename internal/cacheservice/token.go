package cacheservice

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
)

// scope is what a job's runtime token says about which refs it may touch.
//
// GitHub's own cache lets a job read the entries of its own ref and of the
// branches it came from, and write only its own ref, so a pull request cannot
// plant an entry the default branch will later restore. The token carries that
// as an "ac" claim. This service reads the claim and applies the same rule.
//
// The claim is read, not verified: the signing key is GitHub's. That makes the
// rule a guard against a workflow doing the wrong thing, not a boundary against
// code that is trying to, which can forge a token. The real boundary is the
// same as the pool cache's: which jobs share a pool. The docs say so.
type scope struct {
	// write is the ref new entries are filed under; "" when the token names none.
	write string
	// read is every ref whose entries this job may restore, write included.
	read map[string]bool
	// partitioned is false when the token carried no usable claim, in which
	// case every job shares one scope, as it would on the pool cache.
	partitioned bool
}

type accessClaim struct {
	Scope      string `json:"Scope"`
	Permission int    `json:"Permission"`
}

// Permission bits as the token uses them: 1 reads a scope, 2 writes it.
const (
	permRead  = 1
	permWrite = 2
)

// scopeFrom reads the bearer token of a request. A request with no token at
// all is refused by the caller; a token this cannot read is treated as one
// that names no scopes, so a protocol change on GitHub's side makes the cache
// shared rather than broken.
func scopeFrom(r *http.Request) (scope, bool) {
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return scope{}, false
	}
	return scopeFromToken(strings.TrimSpace(token)), true
}

func scopeFromToken(token string) scope {
	none := scope{read: map[string]bool{"": true}}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return none
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return none
	}
	var claims struct {
		AC json.RawMessage `json:"ac"`
	}
	if json.Unmarshal(raw, &claims) != nil || len(claims.AC) == 0 {
		return none
	}
	// GitHub writes the claim as a JSON string holding the array; a plain array
	// is accepted too so a test token need not double-encode it.
	list := claims.AC
	var asString string
	if json.Unmarshal(list, &asString) == nil {
		list = json.RawMessage(asString)
	}
	var acs []accessClaim
	if json.Unmarshal(list, &acs) != nil || len(acs) == 0 {
		return none
	}
	sc := scope{read: map[string]bool{}, partitioned: true}
	for _, ac := range acs {
		if ac.Permission&permRead != 0 {
			sc.read[ac.Scope] = true
		}
		if ac.Permission&permWrite != 0 && sc.write == "" {
			sc.write = ac.Scope
		}
	}
	if sc.write != "" {
		sc.read[sc.write] = true
	}
	return sc
}
