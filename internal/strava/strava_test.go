package strava

import (
	"os"
	"testing"

	"OnelapSyncStrava/internal/config"
)

const testConfigPath = "../../config.json"

func setupConfig(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(testConfigPath); os.IsNotExist(err) {
		t.Skip("config.json not found, skipping integration test. Copy config.sample.json to config.json and fill in credentials.")
	}
	if err := config.LoadConfig(testConfigPath); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
}

// TestRefreshToken verifies that we can refresh the Strava access token.
func TestRefreshToken(t *testing.T) {
	setupConfig(t)

	cfg := &config.GlobalConfig.Strava
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RefreshToken == "" {
		t.Skip("Strava credentials not fully configured, skipping. Run 'go run . auth' first.")
	}

	// Force token refresh by clearing access token
	originalAccessToken := cfg.AccessToken
	originalExpiresAt := cfg.ExpiresAt
	cfg.AccessToken = ""
	cfg.ExpiresAt = 0

	client := NewClient()
	err := client.RefreshToken(testConfigPath)
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	if cfg.AccessToken == "" {
		t.Fatal("RefreshToken succeeded but access token is empty")
	}
	if cfg.ExpiresAt == 0 {
		t.Fatal("RefreshToken succeeded but expires_at is 0")
	}

	t.Logf("Token refreshed successfully:")
	t.Logf("  AccessToken: %s...", cfg.AccessToken[:16])
	t.Logf("  ExpiresAt: %d", cfg.ExpiresAt)

	// Restore original values to avoid unnecessary config writes
	cfg.AccessToken = originalAccessToken
	cfg.ExpiresAt = originalExpiresAt
}

// TestUploadActivity verifies that we can upload a FIT file to Strava.
// This test is skipped by default to avoid creating duplicate activities.
// Run with: go test -run TestUploadActivity -upload-fit=path/to/file.fit
func TestUploadActivity(t *testing.T) {
	fitPath := os.Getenv("TEST_FIT_FILE")
	if fitPath == "" {
		t.Skip("Set TEST_FIT_FILE env var to a .fit file path to test upload. Example: TEST_FIT_FILE=test.fit go test -run TestUploadActivity")
	}

	if _, err := os.Stat(fitPath); os.IsNotExist(err) {
		t.Fatalf("FIT file not found: %s", fitPath)
	}

	setupConfig(t)

	cfg := &config.GlobalConfig.Strava
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RefreshToken == "" {
		t.Skip("Strava credentials not fully configured, skipping. Run 'go run . auth' first.")
	}

	client := NewClient()

	// Ensure token is fresh
	if err := client.RefreshToken(testConfigPath); err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	t.Logf("Uploading FIT file: %s", fitPath)
	if err := client.UploadActivity(fitPath, "test-upload-id", UploadOptions{}); err != nil {
		t.Fatalf("UploadActivity failed: %v", err)
	}

	t.Log("Upload successful! Check your Strava dashboard.")
}
