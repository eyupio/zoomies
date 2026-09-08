package github

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	gh "github.com/google/go-github/v88/github"
)

// GitHub's 401, 403, 404 and 429 all became sentences an operator can act on;
// a 422 -- a runner name already taken, a label GitHub will not accept -- fell
// through as go-github's raw "422 Validation Failed", which is what a runner
// row then showed as the reason it never started.
func TestAValidationFailureSaysWhatGitHubObjectedTo(t *testing.T) {
	cases := []struct {
		name string
		errs []gh.Error
		msg  string
		want string
	}{
		{
			name: "a message per field",
			errs: []gh.Error{{Resource: "Runner", Field: "name", Code: "already_exists", Message: "A runner with this name already exists"}},
			msg:  "Validation Failed",
			want: "A runner with this name already exists",
		},
		{
			name: "a code where there is no message",
			errs: []gh.Error{{Resource: "Runner", Field: "labels", Code: "invalid"}},
			msg:  "Validation Failed",
			want: "Runner labels invalid",
		},
		{
			name: "nothing but the message",
			msg:  "Validation Failed",
			want: "Validation Failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classify(nil, validationError(tc.msg, tc.errs))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("classify = %v, want it to match ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("classify = %q, want it to carry %q", err, tc.want)
			}
		})
	}
}

// The statuses that already had answers keep them: a 422 must not swallow the
// ones a caller branches on.
func TestClassifyKeepsTheOtherStatusesApart(t *testing.T) {
	for status, want := range map[int]error{
		http.StatusNotFound:            ErrNotFound,
		http.StatusForbidden:           ErrForbidden,
		http.StatusTooManyRequests:     ErrRateLimited,
		http.StatusUnprocessableEntity: ErrInvalid,
	} {
		err := classify(nil, &gh.ErrorResponse{Response: response(status), Message: "nope"})
		if !errors.Is(err, want) {
			t.Errorf("classify(%d) = %v, want %v", status, err, want)
		}
	}
}

func validationError(message string, errs []gh.Error) *gh.ErrorResponse {
	return &gh.ErrorResponse{
		Response: response(http.StatusUnprocessableEntity),
		Message:  message,
		Errors:   errs,
	}
}

func response(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Request:    &http.Request{Method: http.MethodPost, URL: &url.URL{Scheme: "https", Host: "api.github.com", Path: "/repos/acme/widgets/actions/runners"}},
	}
}
