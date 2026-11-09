package cacheservice

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// The toolkit's four calls are Twirp over JSON: a POST of a JSON body to
// /twirp/<service>/<method>, answered with JSON. Only the three the cache
// action uses exist here; a call this service does not know is answered as
// Twirp answers an unknown route, and the toolkit treats any failure as a miss.

type twirpError struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

var twirpStatus = map[string]int{
	"malformed":        http.StatusBadRequest,
	"invalid_argument": http.StatusBadRequest,
	"unauthenticated":  http.StatusUnauthorized,
	"bad_route":        http.StatusNotFound,
	"already_exists":   http.StatusConflict,
	"internal":         http.StatusInternalServerError,
}

func writeTwirpError(w http.ResponseWriter, code, msg string) {
	status := twirpStatus[code]
	if status == 0 {
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, twirpError{Code: code, Msg: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type createRequest struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

// Responses use the protobuf field names (signed_upload_url, entry_id), which is
// what GitHub's own service sends. The toolkit's reader takes either spelling;
// BuildKit's Go client takes only these, and reads a missing address as an
// empty one, so a camelCase reply would make every Buildx cache call fail.
type createResponse struct {
	OK              bool   `json:"ok"`
	SignedUploadURL string `json:"signed_upload_url"`
}

// The toolkit sends protobuf field names (size_bytes, restore_keys), not the
// camelCase its own type declarations show; a client built another way may send
// the camelCase JSON names. Both are accepted, because a restore key read as
// absent would make every prefix lookup a silent miss.
type finalizeRequest struct {
	Key            string  `json:"key"`
	Version        string  `json:"version"`
	SizeBytes      flexInt `json:"sizeBytes"`
	SizeBytesProto flexInt `json:"size_bytes"`
}

func (r finalizeRequest) size() string {
	if r.SizeBytesProto != "" {
		return string(r.SizeBytesProto)
	}
	return string(r.SizeBytes)
}

// flexInt is an int64 sent either way the protocol's JSON can carry one: the
// toolkit sends it as a string, as protobuf's JSON mapping says an int64 is, and
// BuildKit's Go client sends a plain number. Both end up as the digits.
type flexInt string

func (f *flexInt) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	if t == "null" {
		return nil
	}
	if len(t) >= 2 && t[0] == '"' && t[len(t)-1] == '"' {
		t = t[1 : len(t)-1]
	}
	*f = flexInt(t)
	return nil
}

type finalizeResponse struct {
	OK      bool   `json:"ok"`
	EntryID string `json:"entry_id"`
}

type downloadRequest struct {
	Key              string   `json:"key"`
	RestoreKeys      []string `json:"restoreKeys"`
	RestoreKeysProto []string `json:"restore_keys"`
	Version          string   `json:"version"`
}

func (r downloadRequest) restoreKeys() []string {
	return append(append([]string(nil), r.RestoreKeys...), r.RestoreKeysProto...)
}

type downloadResponse struct {
	OK                bool   `json:"ok"`
	SignedDownloadURL string `json:"signed_download_url"`
	MatchedKey        string `json:"matched_key"`
}

// maxJSONBody bounds a call's body. The largest legitimate one is a key of 512
// characters and ten restore keys.
const maxJSONBody = 1 << 20

func (s *Service) twirp(w http.ResponseWriter, r *http.Request, method string) {
	if r.Method != http.MethodPost {
		writeTwirpError(w, "bad_route", "Twirp calls are POSTs")
		return
	}
	sc, ok := scopeFrom(r)
	if !ok {
		writeTwirpError(w, "unauthenticated", "a bearer token is required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBody))
	if err != nil {
		writeTwirpError(w, "malformed", "the request body could not be read")
		return
	}
	switch method {
	case "CreateCacheEntry":
		var req createRequest
		if json.Unmarshal(body, &req) != nil || req.Key == "" || req.Version == "" {
			writeTwirpError(w, "invalid_argument", "key and version are required")
			return
		}
		p, ok := s.reserve(sc, req.Key, req.Version)
		if !ok {
			// GitHub answers a key that is already stored or being saved with
			// an error, and the toolkit reads any error as "another job may be
			// creating this cache" and carries on.
			writeTwirpError(w, "already_exists", "an entry with this key and version already exists")
			return
		}
		writeJSON(w, http.StatusOK, createResponse{OK: true, SignedUploadURL: blobURL(r, p.ID)})
	case "FinalizeCacheEntryUpload":
		var req finalizeRequest
		if json.Unmarshal(body, &req) != nil || req.Key == "" || req.Version == "" {
			writeTwirpError(w, "invalid_argument", "key, version and sizeBytes are required")
			return
		}
		size, perr := strconv.ParseInt(req.size(), 10, 64)
		if perr != nil || size < 0 {
			writeTwirpError(w, "invalid_argument", "sizeBytes must be a number")
			return
		}
		e, ok := s.finalise(sc, req.Key, req.Version, size)
		if !ok {
			writeJSON(w, http.StatusOK, finalizeResponse{OK: false})
			return
		}
		writeJSON(w, http.StatusOK, finalizeResponse{OK: true, EntryID: numericID(e.ID)})
	case "GetCacheEntryDownloadURL":
		var req downloadRequest
		if json.Unmarshal(body, &req) != nil || req.Key == "" || req.Version == "" {
			writeTwirpError(w, "invalid_argument", "key and version are required")
			return
		}
		e := s.find(sc, req.Key, req.restoreKeys(), req.Version)
		if e == nil {
			s.count.misses.Add(1)
			writeJSON(w, http.StatusOK, downloadResponse{OK: false})
			return
		}
		s.count.hits.Add(1)
		writeJSON(w, http.StatusOK, downloadResponse{OK: true, SignedDownloadURL: blobURL(r, e.ID), MatchedKey: e.Key})
	default:
		writeTwirpError(w, "bad_route", "no such method")
	}
}

// blobURL is the address the runner is given for one entry's bytes. It is built
// from the host the request came in on, because that is the address the runner
// reached the service by and so the one it can reach again. The id is a random
// capability, like a signed URL; the last segment exists because the storage
// SDK the toolkit uploads with wants a container and a blob name in the path.
func blobURL(r *http.Request, id string) string {
	return "http://" + r.Host + blobPrefix + id + "/archive"
}

// numericID is the entry's id as the protocol wants it: a decimal int64. The
// toolkit parses it as a BigInt and refuses anything else, so the hex id the
// bytes are filed under cannot be sent as it is. Nothing looks an entry up by
// this number; it only has to be stable and fit.
func numericID(id string) string {
	raw, err := hex.DecodeString(id)
	if err != nil || len(raw) < 8 {
		return "0"
	}
	return strconv.FormatUint(binary.BigEndian.Uint64(raw[:8])>>1, 10)
}
