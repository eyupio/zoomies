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
	// gets counts object reads, so a test can say a listing did not open one.
	gets int
	// uploads is the multipart state; a completed upload leaves objects and
	// its parts behind only until completion joins them.
	uploads    map[string]*fakeUpload
	nextUpload int
	// loseComplete stores the joined object and then answers the completion as
	// a failure, which is what a service whose answer was lost looks like.
	loseComplete bool
	// failComplete refuses the completion without storing anything.
	failComplete bool
}

type fakeUpload struct {
	key   string
	parts map[int][]byte
}

type fakeS3Failure struct {
	status  int
	code    string
	message string
}

// NewFakeS3 starts a fake object store holding one bucket. The caller must
// Close it.
func NewFakeS3(bucket string) *FakeS3 {
	f := &FakeS3{objects: map[string][]byte{}, stored: map[string]time.Time{}, uploads: map[string]*fakeUpload{}, bucket: bucket}
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

// Gets is how many objects it has opened, which is how a test proves a
// listing used the key listing alone.
func (f *FakeS3) Gets() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gets
}

// Body is one stored object, for a test that needs to read what landed.
func (f *FakeS3) Body(key string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.objects[key]
	return body, ok
}

// LoseCompletionAnswerOnce makes the next multipart completion store the
// object and then answer as if the reply were lost.
func (f *FakeS3) LoseCompletionAnswerOnce() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loseComplete = true
}

// FailCompletionOnce makes the next multipart completion fail before storing
// anything.
func (f *FakeS3) FailCompletionOnce() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failComplete = true
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
		f.serveObject(w, r, key)
	case http.MethodHead:
		f.mu.Lock()
		body, ok := f.objects[key]
		f.mu.Unlock()
		if !ok {
			f.refuse(w, http.StatusNotFound, "NoSuchKey", "no such key")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
	case http.MethodPost:
		if r.URL.Query().Has("uploads") {
			f.initiateMultipart(w, key)
			return
		}
		if id := r.URL.Query().Get("uploadId"); id != "" {
			f.completeMultipart(w, r, key, id)
			return
		}
		f.refuse(w, http.StatusMethodNotAllowed, "MethodNotAllowed", r.Method)
	case http.MethodPut:
		if id := r.URL.Query().Get("uploadId"); id != "" {
			f.uploadPart(w, r, key, id)
			return
		}
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
		if id := r.URL.Query().Get("uploadId"); id != "" {
			f.mu.Lock()
			delete(f.uploads, id)
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		f.mu.Lock()
		delete(f.objects, key)
		delete(f.stored, key)
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		f.refuse(w, http.StatusMethodNotAllowed, "MethodNotAllowed", r.Method)
	}
}

func (f *FakeS3) serveObject(w http.ResponseWriter, r *http.Request, key string) {
	f.mu.Lock()
	body, ok := f.objects[key]
	f.gets++
	f.mu.Unlock()
	if !ok {
		f.refuse(w, http.StatusNotFound, "NoSuchKey", "no such key")
		return
	}
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		var start, end int64 = 0, int64(len(body)) - 1
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err == nil {
			if start < 0 {
				start = 0
			}
			if end >= int64(len(body)) {
				end = int64(len(body)) - 1
			}
			if start > end || start >= int64(len(body)) {
				f.refuse(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange", "the range is outside the object")
				return
			}
			body = body[start : end+1]
			w.WriteHeader(http.StatusPartialContent)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

func (f *FakeS3) initiateMultipart(w http.ResponseWriter, key string) {
	f.mu.Lock()
	f.nextUpload++
	id := fmt.Sprintf("upload-%d", f.nextUpload)
	f.uploads[id] = &fakeUpload{key: key, parts: map[int][]byte{}}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`,
		xmlEscape(f.bucket), xmlEscape(key), xmlEscape(id))
}

func (f *FakeS3) uploadPart(w http.ResponseWriter, r *http.Request, key, id string) {
	number, err := strconv.Atoi(r.URL.Query().Get("partNumber"))
	if err != nil || number < 1 {
		f.refuse(w, http.StatusBadRequest, "InvalidArgument", "partNumber is not a number")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.refuse(w, http.StatusBadRequest, "IncompleteBody", err.Error())
		return
	}
	declared := r.Header.Get("X-Amz-Content-Sha256")
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != declared {
		f.refuse(w, http.StatusBadRequest, "XAmzContentSHA256Mismatch",
			"the body is not what the request said it would be")
		return
	}
	f.mu.Lock()
	upload, ok := f.uploads[id]
	if !ok || upload.key != key {
		f.mu.Unlock()
		f.refuse(w, http.StatusNotFound, "NoSuchUpload", "no such upload")
		return
	}
	upload.parts[number] = body
	f.mu.Unlock()
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	w.WriteHeader(http.StatusOK)
}

func (f *FakeS3) completeMultipart(w http.ResponseWriter, r *http.Request, key, id string) {
	f.mu.Lock()
	upload, ok := f.uploads[id]
	if !ok || upload.key != key {
		f.mu.Unlock()
		f.refuse(w, http.StatusNotFound, "NoSuchUpload", "no such upload")
		return
	}
	if f.failComplete {
		f.failComplete = false
		delete(f.uploads, id)
		f.mu.Unlock()
		f.refuse(w, http.StatusInternalServerError, "InternalError", "the upload could not be completed")
		return
	}
	numbers := make([]int, 0, len(upload.parts))
	for n := range upload.parts {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	var joined []byte
	for _, n := range numbers {
		joined = append(joined, upload.parts[n]...)
	}
	// A completion's body names the ETags; they are not otherwise needed here.
	_, _ = io.Copy(io.Discard, r.Body)
	f.objects[key] = joined
	f.stored[key] = time.Now().UTC()
	f.puts++
	delete(f.uploads, id)
	lose := f.loseComplete
	if lose {
		f.loseComplete = false
	}
	f.mu.Unlock()
	if lose {
		f.refuse(w, http.StatusInternalServerError, "InternalError", "the answer to the completion was lost")
		return
	}
	sum := sha256.Sum256(joined)
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Key>%s</Key><ETag>"%s"</ETag></CompleteMultipartUploadResult>`,
		xmlEscape(key), hex.EncodeToString(sum[:]))
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
