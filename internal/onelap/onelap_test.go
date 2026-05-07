package onelap

import (
	"fmt"
	"os"
	"path/filepath"
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
	if config.GlobalConfig.Onelap.Account == "" || config.GlobalConfig.Onelap.Password == "" {
		t.Skip("Onelap credentials not configured, skipping integration test.")
	}
}

// TestLogin verifies that we can successfully authenticate with the Onelap API.
func TestLogin(t *testing.T) {
	setupConfig(t)

	client := NewClient()
	err := client.Login(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if client.UID == "" || client.UID == "<nil>" {
		t.Fatal("Login succeeded but UID is empty")
	}
	if client.Token == "" {
		t.Fatal("Login succeeded but Token is empty")
	}

	t.Logf("Login successful: UID=%s, Token=%s...", client.UID, client.Token[:16])
}

// TestGetActivities verifies that we can fetch the activity list after login.
func TestGetActivities(t *testing.T) {
	setupConfig(t)

	client := NewClient()
	if err := client.Login(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Only fetch first 2 pages to avoid a slow full scan in tests.
	activities, err := client.GetRecentActivities(2)
	if err != nil {
		t.Fatalf("GetRecentActivities failed: %v", err)
	}

	t.Logf("Total activities fetched: %d", len(activities))
	for i, act := range activities {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] ID=%s, StartTime=%s, DistanceKm=%.2f", i, act.ExternalID, act.StartTime, act.DistanceKm)
	}
}

// TestGetTodayActivities verifies filtering for today's activities.
func TestGetTodayActivities(t *testing.T) {
	setupConfig(t)

	client := NewClient()
	if err := client.Login(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	activities, err := client.GetTodayActivities()
	if err != nil {
		t.Fatalf("GetTodayActivities failed: %v", err)
	}

	t.Logf("Today's activities: %d", len(activities))
	for i, act := range activities {
		t.Logf("  [%d] ID=%s, StartTime=%s", i, act.ExternalID, act.StartTime)
	}
}

// TestDownloadFIT verifies that we can get a download URL and download a FIT file.
func TestDownloadFIT(t *testing.T) {
	setupConfig(t)

	client := NewClient()
	if err := client.Login(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password); err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	activities, err := client.GetRecentActivities(1)
	if err != nil {
		t.Fatalf("GetRecentActivities failed: %v", err)
	}

	if len(activities) == 0 {
		t.Skip("No activities available to download")
	}

	act := activities[0]
	t.Logf("Fetching download URL for activity: %s", act.ExternalID)

	durl, err := client.GetDownloadURL(act.ExternalID)
	if err != nil {
		t.Fatalf("GetDownloadURL failed: %v", err)
	}
	t.Logf("Download URL: %s", durl)

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, fmt.Sprintf("%s.fit", act.ExternalID))

	if err := client.DownloadFIT(durl, destPath); err != nil {
		t.Fatalf("DownloadFIT failed: %v", err)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatalf("Downloaded file not found: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("Downloaded FIT file is empty")
	}

	t.Logf("Downloaded FIT file: %s (%d bytes)", destPath, info.Size())
}
