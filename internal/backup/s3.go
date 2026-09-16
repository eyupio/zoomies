package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// An S3 client, in four verbs.
//
// There is no SDK behind this for the reason there is no Docker SDK behind
// internal/backend: putting an object, getting it, listing a prefix and
// deleting a key is four signed requests, and the signature is forty lines of
// HMAC. The dependency that implements the other two hundred calls brings a
// transitive tree, a release cadence and a vulnerability surface with it, all
// so that a controller can write one file a night.
//
// It speaks to anything that implements the S3 API -- AWS, MinIO, Ceph,
// Backblaze B2, Cloudflare R2, Garage -- because signature version 4 and
// ListObjectsV2 are what they all agree on.

// s3Client is one bucket at one endpoint, with the credentials that open it.
type s3Client struct {
	endpoint  *url.URL
	bucket    string
	region    string
	accessKey string
	secretKey string
	// pathStyle puts the bucket in the path rather than the hostname. Every
	// self-hosted implementation needs it; AWS's own endpoints do not.
	pathStyle bool
	http      *http.Client
	// now is the clock the signature is dated with, injected so a test can
	// pin one.
	now func() time.Time
}

// defaultS3Region is what a request is signed for when the operator named no
// region. It is not a guess about where the data is: an implementation with no
// regions of its own ignores it, and AWS accepts it as the signing region for
// a bucket it will redirect us to if it is wrong.
const defaultS3Region = "us-east-1"

// s3Timeouts bound the parts of a request that are not the transfer. The
// transfer itself is bounded by the caller's context and nothing else: a
// backup over a slow link takes as long as it takes, and a client timeout that
// killed it at five minutes would fail every night at the same size.
var s3Transport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
	MaxIdleConnsPerHost:   2,
}

// newS3Client builds a client for one remote, or says what is missing.
func newS3Client(endpoint, region, bucket, accessKey, secretKey string, pathStyle *bool, httpClient *http.Client) (*s3Client, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("backup: %q is not an S3 endpoint URL; write one like https://s3.eu-west-2.amazonaws.com or http://minio:9000", endpoint)
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, errors.New("backup: the remote names no bucket")
	}
	if strings.TrimSpace(accessKey) == "" || strings.TrimSpace(secretKey) == "" {
		return nil, errors.New("backup: the remote has no access key and secret key, so every request to it would be refused")
	}
	if region = strings.TrimSpace(region); region == "" {
		region = defaultS3Region
	}
	if httpClient == nil {
		httpClient = &http.Client{Transport: s3Transport}
	}
	style := !isAWSEndpoint(u.Hostname())
	if pathStyle != nil {
		style = *pathStyle
	}
	return &s3Client{
		endpoint: u, bucket: strings.TrimSpace(bucket), region: region,
		accessKey: strings.TrimSpace(accessKey), secretKey: strings.TrimSpace(secretKey),
		pathStyle: style, http: httpClient, now: time.Now,
	}, nil
}

// isAWSEndpoint says whether the host is one of AWS's own, which is the only
// case where virtual-hosted style is the safe default: a bucket name in the
// hostname needs DNS to resolve it, and nothing self-hosted arranges that.
func isAWSEndpoint(host string) bool {
	host = strings.ToLower(host)
	return host == "amazonaws.com" || strings.HasSuffix(host, ".amazonaws.com")
}

// url builds the request URL for a key, in whichever style this endpoint uses.
func (c *s3Client) url(key string) *url.URL {
	u := *c.endpoint
	if c.pathStyle {
		u.Path = "/" + c.bucket
		if key != "" {
			u.Path += "/" + key
		}
		return &u
	}
	u.Host = c.bucket + "." + u.Host
	u.Path = "/" + key
	return &u
}

// s3Object is one key in the bucket, as a listing reports it.
type s3Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

// listResult is ListObjectsV2's answer. Only the fields that are read are
// named: an S3 implementation may send more, and encoding/xml ignores what it
// does not know about.
type listResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
		ETag         string    `xml:"ETag"`
	} `xml:"Contents"`
}

// list returns every key under prefix, following the continuation tokens so a
// bucket with more than a page of backups in it is fully seen.
func (c *s3Client) list(ctx context.Context, prefix string, limit int) ([]s3Object, error) {
	var out []s3Object
	token := ""
	for {
		q := url.Values{}
		q.Set("list-type", "2")
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if limit > 0 {
			q.Set("max-keys", strconv.Itoa(limit))
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		u := c.url("")
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.do(req, emptyPayloadSHA256, 0)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("backup: reading the listing of %s: %w", c.bucket, err)
		}
		var parsed listResult
		if err := xml.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("backup: %s answered a listing this is not: %w", c.endpoint.Host, err)
		}
		for _, item := range parsed.Contents {
			out = append(out, s3Object{
				Key: item.Key, Size: item.Size, LastModified: item.LastModified,
				ETag: strings.Trim(item.ETag, `"`),
			})
		}
		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
		if !parsed.IsTruncated || parsed.NextContinuationToken == "" {
			return out, nil
		}
		token = parsed.NextContinuationToken
	}
}

// put writes one object. The body has to be a reader whose length is known and
// whose contents have already been hashed, because a single signed PUT names
// both: it is what lets the service refuse a transfer that arrived altered
// rather than storing it and telling nobody.
func (c *s3Client) put(ctx context.Context, key string, body io.Reader, size int64, payloadSHA256 string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.url(key).String(), body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.do(req, payloadSHA256, size)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

// get opens one object for reading. The caller closes it.
func (c *s3Client) get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(key).String(), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.do(req, emptyPayloadSHA256, 0)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.ContentLength, nil
}

// remove deletes one object. S3 answers 204 for a key that was never there,
// and so does this: a delete is a statement about the end state.
func (c *s3Client) remove(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.url(key).String(), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req, emptyPayloadSHA256, 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

// do signs a request, sends it, and turns a refusal into a sentence.
func (c *s3Client) do(req *http.Request, payloadSHA256 string, size int64) (*http.Response, error) {
	if err := c.sign(req, payloadSHA256, size); err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("backup: %s %s: %w", req.Method, redactedURL(req.URL), err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return nil, s3Error(req, resp.StatusCode, body)
}

// s3ErrorBody is the document every S3 implementation answers a refusal with.
type s3ErrorBody struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// s3Error turns a status code and an XML body into something an operator can
// act on. The codes named here are the ones that have a fix somebody can
// perform; the rest are reported as the service worded them, because an
// implementation Zoomies has never seen still knows what went wrong.
func s3Error(req *http.Request, status int, body []byte) error {
	var parsed s3ErrorBody
	_ = xml.Unmarshal(body, &parsed)
	where := redactedURL(req.URL)

	switch parsed.Code {
	case "NoSuchBucket":
		return fmt.Errorf("backup: the bucket does not exist at %s. Zoomies never creates one: a backup destination that appeared by itself is one nobody has set the retention or access policy of", where)
	case "AccessDenied":
		return fmt.Errorf("backup: %s refused the request as %s. The credentials work but the policy does not allow it -- a backup remote needs s3:PutObject, s3:GetObject, s3:DeleteObject and s3:ListBucket on the bucket and its prefix", where, req.Method)
	case "InvalidAccessKeyId":
		return fmt.Errorf("backup: %s does not know this access key id", where)
	case "SignatureDoesNotMatch":
		return fmt.Errorf("backup: %s rejected the signature. The secret access key is wrong, or the region the requests are signed for is not the bucket's", where)
	case "RequestTimeTooSkewed":
		return fmt.Errorf("backup: %s refused the request because this host's clock is too far from its own. Fix the clock; nothing signed here will be accepted until it agrees", where)
	case "NoSuchKey":
		return fmt.Errorf("backup: %s holds no such object", where)
	case "PermanentRedirect", "AuthorizationHeaderMalformed":
		return fmt.Errorf("backup: %s says this bucket belongs to another region (%s). Set the remote's region to the bucket's own", where, strings.TrimSpace(parsed.Message))
	}
	switch {
	case status == http.StatusNotFound:
		return fmt.Errorf("backup: %s answered 404; the bucket or the object is not there", where)
	case status == http.StatusForbidden:
		return fmt.Errorf("backup: %s answered 403; the credentials were refused or the policy does not allow %s", where, req.Method)
	case parsed.Message != "":
		return fmt.Errorf("backup: %s answered %d: %s (%s)", where, status, strings.TrimSpace(parsed.Message), parsed.Code)
	default:
		return fmt.Errorf("backup: %s answered %d", where, status)
	}
}

// redactedURL is a URL fit for a log line and an error message: the bucket and
// the key, without the query, because a listing's continuation token is long
// and a presigned parameter would be a credential.
func redactedURL(u *url.URL) string {
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}

// emptyPayloadSHA256 is the digest of no bytes at all, which every request
// without a body is signed with.
const emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// sign adds the signature version 4 headers.
//
// The three headers it sets are all part of what is signed: the host, so a
// signature for one endpoint cannot be replayed against another; the date, so
// it expires; and the payload digest, so a body altered in flight fails to
// verify rather than being stored.
func (c *s3Client) sign(req *http.Request, payloadSHA256 string, size int64) error {
	now := c.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadSHA256)
	if size > 0 {
		req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
	}

	signed, canonicalHeaders := canonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalPath(req.URL),
		canonicalQuery(req.URL),
		canonicalHeaders,
		signed,
		payloadSHA256,
	}, "\n")

	scope := strings.Join([]string{dateStamp, c.region, "s3", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(sha256Of(canonicalRequest)),
	}, "\n")

	key := hmacSHA256([]byte("AWS4"+c.secretKey), dateStamp)
	key = hmacSHA256(key, c.region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.accessKey, scope, signed, signature))
	return nil
}

// canonicalHeaders is the signed header block and the list of names in it.
// Only the three headers that are always present are signed: every extra
// header signed is one a proxy can break by rewriting it.
func canonicalHeaders(req *http.Request) (signed, block string) {
	names := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		value := req.Header.Get(name)
		if name == "host" {
			value = req.URL.Host
		}
		b.WriteString(name)
		b.WriteString(":")
		b.WriteString(strings.TrimSpace(value))
		b.WriteString("\n")
	}
	return strings.Join(names, ";"), b.String()
}

// canonicalPath encodes the path the way the signature expects: every segment
// percent-encoded, the separators left alone, and an empty path spelled "/".
func canonicalPath(u *url.URL) string {
	path := u.Path
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = uriEncode(p, false)
	}
	return strings.Join(parts, "/")
}

// canonicalQuery is the query sorted by name and encoded the same way. Go's
// own Encode is close but not identical -- it escapes a space as "+", which
// the signature counts as a different byte.
func canonicalQuery(u *url.URL) string {
	values := u.Query()
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	var pairs []string
	for _, name := range names {
		vs := values[name]
		sort.Strings(vs)
		for _, v := range vs {
			pairs = append(pairs, uriEncode(name, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(pairs, "&")
}

// uriEncode is the encoding the signature is defined in terms of: everything
// but the unreserved characters is percent-encoded in upper-case hex, and a
// slash is encoded only outside a path.
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'),
			r == '-', r == '_', r == '.', r == '~':
			b.WriteByte(r)
		case r == '/' && !encodeSlash:
			b.WriteByte(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Of(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
