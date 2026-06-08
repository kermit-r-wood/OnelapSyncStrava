package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"OnelapSyncStrava/internal/config"
	"OnelapSyncStrava/internal/strava"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 30, 0, 0, time.Local)

	tests := []struct {
		name  string
		input string
		want  time.Time
	}{
		{"absolute date", "2026-05-01", time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)},
		{"absolute datetime", "2026-05-01 09:30:15", time.Date(2026, 5, 1, 9, 30, 15, 0, time.Local)},
		{"relative days", "7d", time.Date(2026, 5, 17, 0, 0, 0, 0, time.Local)},
		{"relative weeks", "2w", time.Date(2026, 5, 10, 0, 0, 0, 0, time.Local)},
		{"relative months", "6m", time.Date(2025, 11, 24, 0, 0, 0, 0, time.Local)},
		{"relative years", "1y", time.Date(2025, 5, 24, 0, 0, 0, 0, time.Local)},
		{"zero days is today at midnight", "0d", time.Date(2026, 5, 24, 0, 0, 0, 0, time.Local)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSince(tc.input, now)
			if err != nil {
				t.Fatalf("parseSince(%q) unexpected error: %v", tc.input, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("parseSince(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseSinceErrors(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.Local)
	invalid := []string{
		"",
		"abc",
		"7x",         // unknown unit
		"-1d",        // negative not allowed by regex
		"1.5d",       // non-integer
		"d",          // missing number
		"2026/05/01", // wrong separator
	}
	for _, in := range invalid {
		name := in
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if _, err := parseSince(in, now); err == nil {
				t.Errorf("parseSince(%q) expected error, got nil", in)
			}
		})
	}
}

func TestNormalizeStravaUploadMethodAcceptsAPI(t *testing.T) {
	got, err := normalizeStravaUploadMethod("api")
	if err != nil {
		t.Fatalf("normalizeStravaUploadMethod(api) error = %v", err)
	}
	if got != stravaUploadMethodAPI {
		t.Fatalf("normalizeStravaUploadMethod(api) = %q, want %q", got, stravaUploadMethodAPI)
	}
}

func TestNormalizeStravaUploadMethodRejectsUnknown(t *testing.T) {
	_, err := normalizeStravaUploadMethod("ftp")
	if err == nil || !strings.Contains(err.Error(), "strava.upload_method") {
		t.Fatalf("normalizeStravaUploadMethod(ftp) error = %v, want upload_method error", err)
	}
}

func TestResolveStravaUploadMethodDefaultsToWebForNewConfig(t *testing.T) {
	got, err := resolveStravaUploadMethod(config.StravaConfig{})
	if err != nil {
		t.Fatalf("resolveStravaUploadMethod() error = %v", err)
	}
	if got != stravaUploadMethodWeb {
		t.Fatalf("resolveStravaUploadMethod(empty config) = %q, want %q", got, stravaUploadMethodWeb)
	}
}

func TestResolveStravaUploadMethodKeepsLegacyOAuthConfigOnAPI(t *testing.T) {
	got, err := resolveStravaUploadMethod(config.StravaConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		RefreshToken: "refresh",
	})
	if err != nil {
		t.Fatalf("resolveStravaUploadMethod(legacy api config) error = %v", err)
	}
	if got != stravaUploadMethodAPI {
		t.Fatalf("resolveStravaUploadMethod(legacy api config) = %q, want %q", got, stravaUploadMethodAPI)
	}
}

func TestNewConfiguredStravaUploaderRequiresCookieHeaderForWeb(t *testing.T) {
	orig := config.GlobalConfig
	t.Cleanup(func() { config.GlobalConfig = orig })
	config.GlobalConfig.Strava = config.StravaConfig{UploadMethod: "web"}

	_, _, err := newConfiguredStravaUploader()
	want := "strava.web_cookie_header or STRAVA_WEB_COOKIE_HEADER must be set for Strava web upload"
	if err == nil || err.Error() != want {
		t.Fatalf("newConfiguredStravaUploader() error = %v, want %q", err, want)
	}
}

func TestNewConfiguredStravaUploaderAllowsCookieHeaderForWeb(t *testing.T) {
	orig := config.GlobalConfig
	t.Cleanup(func() { config.GlobalConfig = orig })
	config.GlobalConfig.Strava = config.StravaConfig{
		UploadMethod:    "web",
		WebCookieHeader: "Cookie: _strava4_session=session-from-env",
	}

	uploader, method, err := newConfiguredStravaUploader()
	if err != nil {
		t.Fatalf("newConfiguredStravaUploader() error = %v", err)
	}
	if method != stravaUploadMethodWeb {
		t.Fatalf("method = %q, want %q", method, stravaUploadMethodWeb)
	}
	if _, ok := uploader.(*strava.WebClient); !ok {
		t.Fatalf("uploader type = %T, want *strava.WebClient", uploader)
	}
}

func TestSaveConfigPersistsWebCookieHeader(t *testing.T) {
	orig := config.GlobalConfig
	t.Cleanup(func() { config.GlobalConfig = orig })
	config.GlobalConfig = config.Config{
		Strava: config.StravaConfig{
			UploadMethod:    "web",
			WebCookieHeader: "Cookie: _strava4_session=secret",
			ClientID:        "client",
			ClientSecret:    "secret",
			RefreshToken:    "refresh",
			AccessToken:     "access",
			ExpiresAt:       123,
		},
	}

	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"web_cookie_header": "Cookie: _strava4_session=secret"`) {
		t.Fatalf("SaveConfig did not persist web cookie header: %s", data)
	}
}

func TestNewConfiguredStravaUploaderKeepsExplicitAPIFallback(t *testing.T) {
	orig := config.GlobalConfig
	t.Cleanup(func() { config.GlobalConfig = orig })
	config.GlobalConfig.Strava = config.StravaConfig{UploadMethod: "api"}

	uploader, method, err := newConfiguredStravaUploader()
	if err != nil {
		t.Fatalf("newConfiguredStravaUploader() error = %v", err)
	}
	if method != stravaUploadMethodAPI {
		t.Fatalf("method = %q, want %q", method, stravaUploadMethodAPI)
	}
	if _, ok := uploader.(*strava.Client); !ok {
		t.Fatalf("uploader type = %T, want *strava.Client", uploader)
	}
}

func TestNewConfiguredStravaUploaderKeepsLegacyOAuthConfigOnAPI(t *testing.T) {
	orig := config.GlobalConfig
	t.Cleanup(func() { config.GlobalConfig = orig })
	config.GlobalConfig.Strava = config.StravaConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		RefreshToken: "refresh",
	}

	uploader, method, err := newConfiguredStravaUploader()
	if err != nil {
		t.Fatalf("newConfiguredStravaUploader() error = %v", err)
	}
	if method != stravaUploadMethodAPI {
		t.Fatalf("method = %q, want %q", method, stravaUploadMethodAPI)
	}
	if _, ok := uploader.(*strava.Client); !ok {
		t.Fatalf("uploader type = %T, want *strava.Client", uploader)
	}
}

func TestNewConfiguredStravaUploaderDefaultsNewEmptyConfigToWeb(t *testing.T) {
	orig := config.GlobalConfig
	t.Cleanup(func() { config.GlobalConfig = orig })
	config.GlobalConfig.Strava = config.StravaConfig{}

	_, _, err := newConfiguredStravaUploader()
	if err == nil || !strings.Contains(err.Error(), "web_cookie_header") {
		t.Fatalf("newConfiguredStravaUploader() error = %v, want web cookie requirement", err)
	}
}

func TestValidateUploadOptionsRejectsWebOnlyUnsupportedFields(t *testing.T) {
	err := validateUploadOptionsForMethod(stravaUploadMethodWeb, strava.UploadOptions{Commute: true})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("validateUploadOptionsForMethod() error = %v, want unsupported error", err)
	}
}

func TestValidateUploadOptionsAllowsAPIFields(t *testing.T) {
	err := validateUploadOptionsForMethod(stravaUploadMethodAPI, strava.UploadOptions{
		Commute:     true,
		Trainer:     true,
		Name:        "Morning Ride",
		Description: "Uploaded from Onelap",
	})
	if err != nil {
		t.Fatalf("validateUploadOptionsForMethod() error = %v", err)
	}
}

func TestParseUploadFitFlagsRequiresPath(t *testing.T) {
	_, _, err := parseUploadFitFlags(nil)
	if err == nil || !strings.Contains(err.Error(), "fit file path") {
		t.Fatalf("parseUploadFitFlags(nil) error = %v, want path error", err)
	}
}

func TestParseUploadFitFlagsParsesPathAndAPIOptions(t *testing.T) {
	path, opts, err := parseUploadFitFlags([]string{
		"activity.fit",
		"-commute",
		"-trainer",
		"-name=Morning Ride",
		"-description=Uploaded from Onelap",
	})
	if err != nil {
		t.Fatalf("parseUploadFitFlags() error = %v", err)
	}
	if path != "activity.fit" {
		t.Fatalf("path = %q, want activity.fit", path)
	}
	if !opts.Commute || !opts.Trainer || opts.Name != "Morning Ride" || opts.Description != "Uploaded from Onelap" {
		t.Fatalf("opts = %+v", opts)
	}
}
