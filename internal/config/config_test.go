package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigReadsWebCookieHeaderFromConfig(t *testing.T) {
	orig := GlobalConfig
	t.Cleanup(func() { GlobalConfig = orig })

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"strava":{"upload_method":"web","web_cookie_header":"Cookie: _strava4_session=config"}}`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := LoadConfig(path); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if GlobalConfig.Strava.WebCookieHeader != "Cookie: _strava4_session=config" {
		t.Fatalf("WebCookieHeader = %q", GlobalConfig.Strava.WebCookieHeader)
	}
	if err := SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"web_cookie_header": "Cookie: _strava4_session=config"`) {
		t.Fatalf("SaveConfig did not persist cookie header: %s", data)
	}
}

func TestLoadConfigLetsEnvironmentOverrideWebCookieHeader(t *testing.T) {
	orig := GlobalConfig
	t.Cleanup(func() { GlobalConfig = orig })

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"strava":{"upload_method":"web","web_cookie_header":"Cookie: _strava4_session=config"}}`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("STRAVA_WEB_COOKIE_HEADER", "Cookie: _strava4_session=env")

	if err := LoadConfig(path); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if GlobalConfig.Strava.WebCookieHeader != "Cookie: _strava4_session=env" {
		t.Fatalf("WebCookieHeader = %q", GlobalConfig.Strava.WebCookieHeader)
	}
}

func TestSaveConfigDropsDeprecatedBrowserHeaderConfig(t *testing.T) {
	orig := GlobalConfig
	t.Cleanup(func() { GlobalConfig = orig })
	GlobalConfig = Config{}

	deprecatedKey := "web_" + "user_agent"
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"strava":{"upload_method":"web","web_cookie_header":"Cookie: _strava4_session=config","`+deprecatedKey+`":"custom"}}`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("STRAVA_WEB_"+"USER_AGENT", "custom-env")

	if err := LoadConfig(path); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), deprecatedKey) {
		t.Fatalf("SaveConfig persisted deprecated browser header config: %s", data)
	}
}
