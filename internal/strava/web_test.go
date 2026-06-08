package strava

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebClientUploadsFITWithFreshCSRFAndSessionCookie(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "activity.fit")
	const fitBytes = "fit-binary"
	if err := os.WriteFile(filePath, []byte(fitBytes), 0644); err != nil {
		t.Fatalf("write fit: %v", err)
	}

	var posted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/select":
			if r.Method != http.MethodGet {
				t.Fatalf("/upload/select method = %s, want GET", r.Method)
			}
			http.SetCookie(w, &http.Cookie{Name: "_strava4_session", Value: "session-from-select", Path: "/"})
			fmt.Fprint(w, `<html><head><meta name="csrf-token" content="fresh-csrf"></head></html>`)
		case "/upload/files":
			posted = true
			if r.Method != http.MethodPost {
				t.Fatalf("/upload/files method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("X-CSRF-Token"); got != "fresh-csrf" {
				t.Fatalf("X-CSRF-Token = %q, want fresh-csrf", got)
			}
			if got := r.Header.Get("X-Requested-With"); got != "XMLHttpRequest" {
				t.Fatalf("X-Requested-With = %q, want XMLHttpRequest", got)
			}
			if got := r.Header.Get("Origin"); got != serverOrigin(r) {
				t.Fatalf("Origin = %q, want %q", got, serverOrigin(r))
			}
			if got := r.Header.Get("Referer"); got != serverOrigin(r)+"/upload/select" {
				t.Fatalf("Referer = %q, want upload select referer", got)
			}
			cookie, err := r.Cookie("_strava4_session")
			if err != nil || cookie.Value != "session-from-select" {
				t.Fatalf("session cookie = %v, %v; want session-from-select", cookie, err)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("ParseMultipartForm: %v", err)
			}
			assertFormValue(t, r.MultipartForm.Value, "_method", "post")
			assertFormValue(t, r.MultipartForm.Value, "authenticity_token", "fresh-csrf")
			files := r.MultipartForm.File["files[]"]
			if len(files) != 1 {
				t.Fatalf("files[] count = %d, want 1", len(files))
			}
			if files[0].Filename != "activity.fit" {
				t.Fatalf("uploaded filename = %q, want activity.fit", files[0].Filename)
			}
			f, err := files[0].Open()
			if err != nil {
				t.Fatalf("open uploaded file: %v", err)
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				t.Fatalf("read uploaded file: %v", err)
			}
			if string(data) != fitBytes {
				t.Fatalf("uploaded file body = %q, want %q", data, fitBytes)
			}
			fmt.Fprint(w, "ok")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewWebClient(WebOptions{
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewWebClient: %v", err)
	}
	if err := client.UploadActivity(filePath, "external-id", UploadOptions{}); err != nil {
		t.Fatalf("UploadActivity() error = %v", err)
	}
	if !posted {
		t.Fatal("UploadActivity() did not POST /upload/files")
	}
}

func TestWebClientLoadsCookieHeaderOptionBeforeFetchingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/upload/select" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		session, err := r.Cookie("_strava4_session")
		if err != nil || session.Value != "cookie-from-option" {
			t.Fatalf("session cookie = %v, %v; want cookie-from-option", session, err)
		}
		fmt.Fprint(w, `<meta name="csrf-token" content="option-csrf">`)
	}))
	defer server.Close()

	client, err := NewWebClient(WebOptions{
		BaseURL:      server.URL,
		CookieHeader: "Cookie: _strava4_session=cookie-from-option",
	})
	if err != nil {
		t.Fatalf("NewWebClient: %v", err)
	}
	if err := client.Check(); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestExtractCSRFTokenAcceptsMetaAndHiddenInput(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "meta name before content",
			html: `<meta name="csrf-token" content="meta-token">`,
			want: "meta-token",
		},
		{
			name: "meta content before name",
			html: `<meta content="reordered-token" name="csrf-token">`,
			want: "reordered-token",
		},
		{
			name: "hidden input",
			html: `<input value="hidden-token" name="authenticity_token">`,
			want: "hidden-token",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractCSRFToken(tc.html)
			if err != nil {
				t.Fatalf("ExtractCSRFToken() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("ExtractCSRFToken() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWebClientRejectsUnsupportedUploadOptions(t *testing.T) {
	client, err := NewWebClient(WebOptions{BaseURL: "https://www.strava.com"})
	if err != nil {
		t.Fatalf("NewWebClient: %v", err)
	}
	err = client.UploadActivity("activity.fit", "external-id", UploadOptions{Name: "Morning Ride"})
	if err == nil || !strings.Contains(err.Error(), "not supported by Strava web upload") {
		t.Fatalf("UploadActivity() error = %v, want unsupported options error", err)
	}
}

func TestWebClientTreatsUploadRedirectToLoginAsFailure(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "activity.fit")
	if err := os.WriteFile(filePath, []byte("fit"), 0644); err != nil {
		t.Fatalf("write fit: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/select":
			fmt.Fprint(w, `<meta name="csrf-token" content="fresh-csrf">`)
		case "/upload/files":
			http.Redirect(w, r, "/login", http.StatusFound)
		default:
			fmt.Fprint(w, "login page")
		}
	}))
	defer server.Close()

	client, err := NewWebClient(WebOptions{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewWebClient: %v", err)
	}
	err = client.UploadActivity(filePath, "external-id", UploadOptions{})
	if err == nil || !strings.Contains(err.Error(), "redirected") {
		t.Fatalf("UploadActivity() error = %v, want redirected failure", err)
	}
}

func serverOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
