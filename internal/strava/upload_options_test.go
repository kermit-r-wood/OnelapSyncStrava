package strava

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"OnelapSyncStrava/internal/config"
)

func TestUploadActivitySendsOnlyConfiguredUploadOptions(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "activity.fit")
	if err := os.WriteFile(filePath, []byte("fit"), 0644); err != nil {
		t.Fatalf("write fit: %v", err)
	}

	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		form = r.MultipartForm.Value
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	config.GlobalConfig.Strava.AccessToken = "token"
	client := NewClient()
	client.restyClient.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = serverURL.Scheme
		r.URL.Host = serverURL.Host
		return http.DefaultTransport.RoundTrip(r)
	}))
	if err := client.UploadActivity(filePath, "external-id", UploadOptions{
		Commute: true,
		Name:    "Morning Ride",
	}); err != nil {
		t.Fatalf("UploadActivity() error = %v", err)
	}

	assertFormValue(t, form, "data_type", "fit")
	assertFormValue(t, form, "external_id", "external-id")
	assertFormValue(t, form, "commute", "1")
	assertFormValue(t, form, "name", "Morning Ride")
	assertFormAbsent(t, form, "trainer")
	assertFormAbsent(t, form, "description")
}

func assertFormValue(t *testing.T, form map[string][]string, key, want string) {
	t.Helper()
	got := form[key]
	if len(got) != 1 || got[0] != want {
		t.Fatalf("form[%q] = %v, want [%q]", key, got, want)
	}
}

func assertFormAbsent(t *testing.T, form map[string][]string, key string) {
	t.Helper()
	if got, ok := form[key]; ok {
		t.Fatalf("form[%q] = %v, want absent", key, got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
