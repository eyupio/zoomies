package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FakeS3 is a fake object store, in the spirit of internal/github's fake
// GitHub and for the same reason: the tests of everything that ships a backup
// -- this package's, the controller's loop, the API's handlers -- should
// exercise the real client against a real socket rather than a stub of
// themselves.
//
// It is enough of the protocol to be useful and strict about the parts that
// matter: every request must be signed, dated and must state its payload, and
// a PUT whose body is not the digest it declared is refused. A client that
// quietly stopped signing would fail here rather than in somebody's bucket.
//
// It lives in a non-test file because packages above this one need it, and it
// is path style because that is what a bare host with no wildcard DNS in front
// of it can be.
type FakeS3 struct {
	server *httptest.Server
	mu     sync.Mutex
	// objects is the bucket, keyed by object key.
	objects map[string][]byte
	stored  map[string]time.Time
	// bucket is the one bucket that exists; anything else is NoSuchBucket.
	bucket string
	// fail, when set, is answered to every request: the way a remote that is
	// down is simulated.
	fail *fakeS3Failure
	// puts counts successful writes, so a test can say the catch-up sent two.
	puts int
}

type fakeS3Failure struct {
	status  int
	code    string
	message string
}

// NewFakeS3 starts a fake object store holding one bucket. The caller must
// Close it.
func NewFakeS3(bucket string) *FakeS3 {
	f := &FakeS3{objects: map[string][]byte{}, stored: map[string]time.Time{}, bucket: bucket}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

// Close stops the fake.
func (f *FakeS3) Close() { f.server.Close() }

// Endpoint is what a remote's configuration points at.
func (f *FakeS3) Endpoint() string { return f.server.URL }

// Bucket is the one bucket it holds.
func (f *FakeS3) Bucket() string { return f.bucket }

// BreakWith makes every request from now on fail with this S3 error, which is
// how a remote that is down, refusing a signature or missing its bucket is
// arranged.
func (f *FakeS3) BreakWith(status int, code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = &fakeS3Failure{status: status, code: code, message: message}
}

// Mend puts it back.
func (f *FakeS3) Mend() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = nil
}

// Keys is every object key it holds, sorted.
func (f *FakeS3) Keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.objects))
	for k := range f.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Count is how many objects it holds.
func (f *FakeS3) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects)
}

// Puts is how many successful writes it has taken, so a test can say that a
// catch-up sent two backups rather than one.
func (f *FakeS3) Puts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts
}

func (f *FakeS3) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	failure := f.fail
	f.mu.Unlock()
	if failure != nil {
		f.refuse(w, failure.status, failure.code, failure.message)
		return
	}

	// Every request is signed, dated and states its payload. A client that
	// stopped doing any of the three would be refused by a real service, so it
	// is refused here.
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") {
		f.refuse(w, http.StatusForbidden, "AccessDenied", "the request is not signed")
		return
	}
	if r.Header.Get("X-Amz-Date") == "" {
		f.refuse(w, http.StatusForbidden, "AccessDenied", "the request has no date")
		return
	}
	declared := r.Header.Get("X-Amz-Content-Sha256")
	if declared == "" {
		f.refuse(w, http.StatusForbidden, "AccessDenied", "the request does not state its payload")
		return
	}

	// The fake is path style, which is what a bare host with no wildcard DNS
	// in front of it can be.
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(trimmed, "/")
	if bucket != f.bucket {
		f.refuse(w, http.StatusNotFound, "NoSuchBucket", "no such bucket")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if key == "" {
			f.listObjects(w, r)
			return
		}
		f.mu.Lock()
		body, ok := f.objects[key]
		f.mu.Unlock()
		if !ok {
			f.refuse(w, http.StatusNotFound, "NoSuchKey", "no such key")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			f.refuse(w, http.StatusBadRequest, "IncompleteBody", err.Error())
			return
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != declared {
			f.refuse(w, http.StatusBadRequest, "XAmzContentSHA256Mismatch",
				"the body is not what the request said it would be")
			return
		}
		f.mu.Lock()
		f.objects[key] = body
		f.stored[key] = time.Now().UTC()
		f.puts++
		f.mu.Unlock()
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		f.mu.Lock()
		delete(f.objects, key)
		delete(f.stored, key)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		f.refuse(w, http.StatusMethodNotAllowed, "MethodNotAllowed", r.Method)
	}
}

// listObjects answers ListObjectsV2, one key at a time when a max-keys is
// given, so the client's continuation handling is exercised rather than
// assumed.
func (f *FakeS3) listObjects(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("list-type") != "2" {
		f.refuse(w, http.StatusBadRequest, "InvalidArgument", "only ListObjectsV2 is implemented")
		return
	}
	prefix := r.URL.Query().Get("prefix")
	after := r.URL.Query().Get("continuation-token")
	page := 1
	if v := r.URL.Query().Get("max-keys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}

	f.mu.Lock()
	keys := make([]string, 0, len(f.objects))
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) && k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult>`)
	truncated := len(keys) > page
	shown := keys
	if truncated {
		shown = keys[:page]
	}
	for _, k := range shown {
		sum := sha256.Sum256(f.objects[k])
		fmt.Fprintf(&body, `<Contents><Key>%s</Key><Size>%d</Size><LastModified>%s</LastModified><ETag>"%s"</ETag></Contents>`,
			xmlEscape(k), len(f.objects[k]), f.stored[k].Format(time.RFC3339), hex.EncodeToString(sum[:]))
	}
	f.mu.Unlock()

	fmt.Fprintf(&body, `<IsTruncated>%t</IsTruncated>`, truncated)
	if truncated {
		fmt.Fprintf(&body, `<NextContinuationToken>%s</NextContinuationToken>`, xmlEscape(shown[len(shown)-1]))
	}
	body.WriteString(`</ListBucketResult>`)
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, body.String())
}

func (f *FakeS3) refuse(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`,
		xmlEscape(code), xmlEscape(message))
}

// Put writes an object directly, for the test that needs a bucket to already
// hold something -- somebody else's backups, most usefully.
func (f *FakeS3) Put(key string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = body
	f.stored[key] = time.Now().UTC()
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
