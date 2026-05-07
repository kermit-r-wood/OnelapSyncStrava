package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"OnelapSyncStrava/internal/config"
	"OnelapSyncStrava/internal/onelap"
	"OnelapSyncStrava/internal/strava"
)

const (
	configPath = "config.json"
	statePath  = "state.json"
)

func main() {
	if err := config.LoadConfig(configPath); err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	if err := config.LoadState(statePath); err != nil {
		log.Fatalf("Error loading state: %v", err)
	}

	command := "sync"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "auth":
		if err := strava.Authorize(configPath); err != nil {
			log.Fatalf("Strava authorization error: %v", err)
		}
	case "check":
		runCheck()
	case "status":
		runStatus()
	case "sync":
		runSync()
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("OnelapSyncStrava - Sync Onelap activities to Strava")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  OnelapSyncStrava [command]")
	fmt.Println("\nAvailable Commands:")
	fmt.Println("  sync    (default) Fetch today's activities and upload to Strava")
	fmt.Println("  auth    Run Strava OAuth flow to get access tokens")
	fmt.Println("  check   Verify credentials and connectivity")
	fmt.Println("  status  Show current configuration and sync status")
}

func runCheck() {
	onelapClient := onelap.NewClient()
	fmt.Print("Checking Onelap connectivity... ")
	if err := onelapClient.Check(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password); err != nil {
		fmt.Printf("FAILED: %v\n", err)
	} else {
		fmt.Println("SUCCESS")
	}

	stravaClient := strava.NewClient()
	fmt.Print("Checking Strava connectivity...  ")
	if err := stravaClient.Check(configPath); err != nil {
		fmt.Printf("FAILED: %v\n", err)
	} else {
		fmt.Println("SUCCESS")
	}
}

func runStatus() {
	fmt.Println("--- Configuration Status ---")
	fmt.Printf("Onelap Account:  %s\n", config.GlobalConfig.Onelap.Account)
	fmt.Printf("Strava ClientID: %s\n", config.GlobalConfig.Strava.ClientID)
	
	hasToken := "No"
	if config.GlobalConfig.Strava.RefreshToken != "" {
		hasToken = "Yes"
	}
	fmt.Printf("Strava Authed:   %s\n", hasToken)
	
	fmt.Printf("\n--- Sync Status ---\n")
	fmt.Printf("Synced Activities: %d\n", len(config.GlobalState.SyncedIDs))
}

func runSync() {
	onelapClient := onelap.NewClient()
	stravaClient := strava.NewClient()

	// 1. Login to Onelap
	log.Println("Logging in to Onelap...")
	if err := onelapClient.Login(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password); err != nil {
		log.Fatalf("Onelap login error: %v", err)
	}

	// 2. Get today's activities
	log.Println("Fetching today's activities from Onelap...")
	activities, err := onelapClient.GetTodayActivities()
	if err != nil {
		log.Fatalf("Error getting today's activities: %v", err)
	}

	if len(activities) == 0 {
		log.Println("No activities found for today.")
		return
	}

	log.Printf("Found %d activities to check.", len(activities))

	// 3. Refresh Strava token
	log.Println("Refreshing Strava token...")
	if err := stravaClient.RefreshToken(configPath); err != nil {
		log.Fatalf("Strava token refresh error: %v", err)
	}

	// 4. Download and Upload
	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		log.Fatalf("Error creating tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	syncedCount := 0
	for _, act := range activities {
		idStr := act.ExternalID

		if config.IsSynced(idStr) {
			log.Printf("Activity %s already synced, skipping.", idStr)
			continue
		}

		log.Printf("Processing activity: %s (%s)", idStr, act.StartTime)

		// Fetch the pre-signed download URL via the analysis endpoint.
		log.Printf("Fetching download URL...")
		durl, err := onelapClient.GetDownloadURL(idStr)
		if err != nil {
			log.Printf("Error getting download URL for activity %s: %v", idStr, err)
			continue
		}

		fitPath := filepath.Join(tmpDir, fmt.Sprintf("%s.fit", idStr))
		log.Printf("Downloading FIT file...")
		if err := onelapClient.DownloadFIT(durl, fitPath); err != nil {
			log.Printf("Error downloading FIT for activity %s: %v", idStr, err)
			continue
		}

		log.Printf("Uploading to Strava...")
		if err := stravaClient.UploadActivity(fitPath, idStr); err != nil {
			log.Printf("Error uploading to Strava: %v", err)
		} else {
			log.Printf("Successfully synced activity %s", idStr)
			config.AddSyncedID(idStr)
			syncedCount++
		}
	}

	if syncedCount > 0 {
		if err := config.SaveState(statePath); err != nil {
			log.Printf("Warning: failed to save state: %v", err)
		}
	}
	log.Printf("Sync complete. %d new activities synced.", syncedCount)
}
