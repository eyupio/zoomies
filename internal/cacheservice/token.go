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
// The claim is read, not verified: the signing key is GitHub's, and a host that
// cannot reach GitHub's key set on a job's behalf has no way to check it. That
// makes the rule a guard against a workflow doing the wrong thing, not a
// boundary against code that is trying to, which can forge a token with any ac
// claim it likes, or share the whole cache by sending one with none. The most
// this service does about that is refuse a token that is not even shaped like
// one (it can neither read nor write) and a token with no write scope (it can
// not write), so the residual limit is exactly this: a well-formed, forged token
// reaches every entry of the pool it is sent to. The real boundary is the same
// as the pool cache's: which jobs share a pool, because each pool, and each
// repository of a repository-scoped pool, has its own listener. The docs say so.
type scope struct {
	// write is the ref new entries are filed under; "" when the token names none.
	write string
	// read is every ref whose entries this job may restore, write included.
	read map[string]bool
	// partitioned is false when the token carried no usable claim, in which
	// case every job shares one scope, as it would on the pool cache.
	partitioned bool
	// valid is false for a token that is not shaped like a JWT with a JSON
	// payload. Such a token reads nothing and writes nothing, so every call it
	// makes is a miss or a failed save and never a failed build.
	valid bool
}

// canWrite says whether this token may start a save at all. A token with no
// claim shares one scope; a partitioned one needs a scope it has write
// permission on, because filing a read-only job's entry under "" would put it
// in the scope every claimless token reads.
func (sc scope) canWrite() bool {
	return sc.valid && (!sc.partitioned || sc.write != "")
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
// all is refused by the caller; a well-formed token whose claim this cannot read
// is treated as one that names no scopes, so a protocol change on GitHub's side
// makes the cache shared rather than broken, and a malformed one gets nothing.
func scopeFrom(r *http.Request) (scope, bool) {
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return scope{}, false
	}
	return scopeFromToken(strings.TrimSpace(token)), true
}

func scopeFromToken(token string) scope {
	// A well-formed token that names no scopes: GitHub changed the claim, and the
	// cache becomes shared rather than broken.
	none := scope{read: map[string]bool{"": true}, valid: true}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return scope{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return scope{}
	}
	var claims struct {
		AC json.RawMessage `json:"ac"`
	}
	if json.Unmarshal(raw, &claims) != nil {
		return scope{}
	}
	if len(claims.AC) == 0 {
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
	sc := scope{read: map[string]bool{}, partitioned: true, valid: true}
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
