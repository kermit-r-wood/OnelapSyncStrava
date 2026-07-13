package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"OnelapSyncStrava/internal/config"
	"OnelapSyncStrava/internal/fitconv"
	"OnelapSyncStrava/internal/onelap"
	"OnelapSyncStrava/internal/strava"
)

var relativeSinceRE = regexp.MustCompile(`^(\d+)([dwmy])$`)

const (
	configPath = "config.json"
	statePath  = "state.json"
)

const (
	defaultUploadBatchSize = 15
	defaultUploadDelay     = 2 * time.Minute
)

type stravaUploadMethod string

const (
	stravaUploadMethodWeb stravaUploadMethod = "web"
	stravaUploadMethodAPI stravaUploadMethod = "api"
)

type stravaUploader interface {
	UploadActivity(filePath, externalID string, opts strava.UploadOptions) error
}

type stravaBatchUploader interface {
	UploadActivities(filePaths []string, opts strava.UploadOptions) error
}

type uploadPacing struct {
	BatchSize int
	Delay     time.Duration
}

type uploadItem struct {
	Path       string
	ExternalID string
}

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
		var syncArgs []string
		if len(os.Args) > 2 {
			syncArgs = os.Args[2:]
		}
		since, opts, pacing := parseSyncFlags(syncArgs)
		runSync(since, opts, pacing)
	case "upload-fit":
		path, opts, pacing, err := parseUploadFitFlags(os.Args[2:])
		if err != nil {
			log.Fatalf("Invalid upload-fit arguments: %v", err)
		}
		runUploadFit(path, opts, pacing)
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
	fmt.Println("          Default Strava upload method is web; set strava.upload_method=api for OAuth API upload")
	fmt.Println("          -since=2026-05-01   Sync activities on or after this date")
	fmt.Println("          -since=7d           Sync the last 7 days  (also: Nw / Nm / Ny — e.g. 6m, 1y)")
	fmt.Println("          -commute            API mode only: mark uploaded activities as commute")
	fmt.Println("          -trainer            API mode only: mark uploaded activities as trainer/indoor")
	fmt.Println("          -name=\"Morning Ride\" API mode only: override the activity name on Strava")
	fmt.Println("          -description=\"...\"  API mode only: set the activity description on Strava")
	fmt.Println("          -upload-batch=15    Upload at most this many files before waiting")
	fmt.Println("          -upload-delay=2m    Wait between upload batches")
	fmt.Println("  auth    API mode only: run Strava OAuth flow to get access tokens")
	fmt.Println("  check   Verify credentials and connectivity")
	fmt.Println("  status  Show current configuration and sync status")
	fmt.Println("  upload-fit <path>  Upload one local FIT file or a directory of FIT files")
}

func runCheck() {
	onelapClient := onelap.NewClient()
	fmt.Print("Checking Onelap connectivity... ")
	if err := onelapClient.Check(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password); err != nil {
		fmt.Printf("FAILED: %v\n", err)
	} else {
		fmt.Println("SUCCESS")
	}

	fmt.Print("Checking Strava connectivity...  ")
	if err := checkConfiguredStrava(configPath); err != nil {
		fmt.Printf("FAILED: %v\n", err)
	} else {
		fmt.Println("SUCCESS")
	}
}

func runStatus() {
	fmt.Println("--- Configuration Status ---")
	fmt.Printf("Onelap Account:  %s\n", config.GlobalConfig.Onelap.Account)
	method, err := resolveStravaUploadMethod(config.GlobalConfig.Strava)
	if err != nil {
		method = "invalid"
	}
	fmt.Printf("Strava Upload:   %s\n", method)
	if method == stravaUploadMethodWeb {
		hasCookie := "No"
		if config.GlobalConfig.Strava.WebCookieHeader != "" {
			hasCookie = "Yes"
		}
		fmt.Printf("Strava Cookie:   %s\n", hasCookie)
	} else {
		fmt.Printf("Strava ClientID: %s\n", config.GlobalConfig.Strava.ClientID)
		hasToken := "No"
		if config.GlobalConfig.Strava.RefreshToken != "" {
			hasToken = "Yes"
		}
		fmt.Printf("Strava Authed:   %s\n", hasToken)
	}

	fmt.Printf("\n--- Sync Status ---\n")
	fmt.Printf("Synced Activities: %d\n", len(config.GlobalState.SyncedIDs))
}

func normalizeStravaUploadMethod(method string) (stravaUploadMethod, error) {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "", string(stravaUploadMethodWeb):
		return stravaUploadMethodWeb, nil
	case string(stravaUploadMethodAPI):
		return stravaUploadMethodAPI, nil
	default:
		return "", fmt.Errorf("invalid strava.upload_method %q (expected %q or %q)", method, stravaUploadMethodWeb, stravaUploadMethodAPI)
	}
}

func resolveStravaUploadMethod(cfg config.StravaConfig) (stravaUploadMethod, error) {
	if strings.TrimSpace(cfg.UploadMethod) != "" {
		return normalizeStravaUploadMethod(cfg.UploadMethod)
	}
	if hasLegacyStravaOAuthConfig(cfg) {
		return stravaUploadMethodAPI, nil
	}
	return stravaUploadMethodWeb, nil
}

func hasLegacyStravaOAuthConfig(cfg config.StravaConfig) bool {
	return strings.TrimSpace(cfg.ClientID) != "" &&
		strings.TrimSpace(cfg.ClientSecret) != "" &&
		strings.TrimSpace(cfg.RefreshToken) != ""
}

func newConfiguredStravaUploader() (stravaUploader, stravaUploadMethod, error) {
	method, err := resolveStravaUploadMethod(config.GlobalConfig.Strava)
	if err != nil {
		return nil, "", err
	}
	switch method {
	case stravaUploadMethodAPI:
		return strava.NewClient(), method, nil
	case stravaUploadMethodWeb:
		cfg := config.GlobalConfig.Strava
		if cfg.WebCookieHeader == "" {
			return nil, "", fmt.Errorf("strava.web_cookie_header or STRAVA_WEB_COOKIE_HEADER must be set for Strava web upload")
		}
		client, err := strava.NewWebClient(strava.WebOptions{
			CookieHeader: cfg.WebCookieHeader,
		})
		if err != nil {
			return nil, "", err
		}
		return client, method, nil
	default:
		return nil, "", fmt.Errorf("unsupported Strava upload method %q", method)
	}
}

func validateUploadOptionsForMethod(method stravaUploadMethod, opts strava.UploadOptions) error {
	if method == stravaUploadMethodWeb && (opts.Commute || opts.Trainer || opts.Name != "" || opts.Description != "") {
		return fmt.Errorf("commute, trainer, name and description are not supported by Strava web upload; set strava.upload_method to %q to use these flags", stravaUploadMethodAPI)
	}
	return nil
}

func checkConfiguredStrava(configPath string) error {
	method, err := resolveStravaUploadMethod(config.GlobalConfig.Strava)
	if err != nil {
		return err
	}
	switch method {
	case stravaUploadMethodAPI:
		return strava.NewClient().Check(configPath)
	case stravaUploadMethodWeb:
		uploader, _, err := newConfiguredStravaUploader()
		if err != nil {
			return err
		}
		return uploader.(*strava.WebClient).Check()
	default:
		return fmt.Errorf("unsupported Strava upload method %q", method)
	}
}

func parseSyncFlags(args []string) (time.Time, strava.UploadOptions, uploadPacing) {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	since := fs.String("since", "", "Sync activities on or after this date. Accepts YYYY-MM-DD (e.g. 2026-05-01) or a relative duration: Nd / Nw / Nm / Ny (e.g. 7d, 6m). Default: today + yesterday.")
	commute := fs.Bool("commute", false, "Mark uploaded activities as commute on Strava.")
	trainer := fs.Bool("trainer", false, "Mark uploaded activities as trainer/indoor on Strava.")
	name := fs.String("name", "", "Override the activity name on Strava.")
	description := fs.String("description", "", "Set the activity description on Strava.")
	uploadBatch := fs.Int("upload-batch", defaultUploadBatchSize, "Upload at most this many files before waiting.")
	uploadDelay := fs.Duration("upload-delay", defaultUploadDelay, "Wait between upload batches, e.g. 30m, 12h, 0s.")
	if err := fs.Parse(args); err != nil {
		log.Fatalf("Failed to parse sync flags: %v", err)
	}
	pacing, err := newUploadPacing(*uploadBatch, *uploadDelay)
	if err != nil {
		log.Fatalf("Invalid upload pacing: %v", err)
	}
	opts := strava.UploadOptions{
		Commute:     *commute,
		Trainer:     *trainer,
		Name:        *name,
		Description: *description,
	}
	if *since == "" {
		return time.Time{}, opts, pacing
	}
	t, err := parseSince(*since, time.Now())
	if err != nil {
		log.Fatalf("Invalid -since value: %v", err)
	}
	return t, opts, pacing
}

func parseUploadFitFlags(args []string) (string, strava.UploadOptions, uploadPacing, error) {
	fs := flag.NewFlagSet("upload-fit", flag.ContinueOnError)
	commute := fs.Bool("commute", false, "API mode only: mark uploaded activity as commute on Strava.")
	trainer := fs.Bool("trainer", false, "API mode only: mark uploaded activity as trainer/indoor on Strava.")
	name := fs.String("name", "", "API mode only: override the activity name on Strava.")
	description := fs.String("description", "", "API mode only: set the activity description on Strava.")
	uploadBatch := fs.Int("upload-batch", defaultUploadBatchSize, "Upload at most this many files before waiting.")
	uploadDelay := fs.Duration("upload-delay", defaultUploadDelay, "Wait between upload batches, e.g. 30m, 12h, 0s.")
	orderedArgs, err := reorderUploadFitArgs(args)
	if err != nil {
		return "", strava.UploadOptions{}, uploadPacing{}, err
	}
	if err := fs.Parse(orderedArgs); err != nil {
		return "", strava.UploadOptions{}, uploadPacing{}, err
	}
	if fs.NArg() != 1 {
		return "", strava.UploadOptions{}, uploadPacing{}, fmt.Errorf("expected exactly one fit file path or directory")
	}
	pacing, err := newUploadPacing(*uploadBatch, *uploadDelay)
	if err != nil {
		return "", strava.UploadOptions{}, uploadPacing{}, err
	}
	return fs.Arg(0), strava.UploadOptions{
		Commute:     *commute,
		Trainer:     *trainer,
		Name:        *name,
		Description: *description,
	}, pacing, nil
}

func reorderUploadFitArgs(args []string) ([]string, error) {
	var flags []string
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		if (name == "name" || name == "description" || name == "upload-batch" || name == "upload-delay") && !strings.Contains(arg, "=") {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag needs an argument: -%s", name)
			}
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...), nil
}

func newUploadPacing(batchSize int, delay time.Duration) (uploadPacing, error) {
	if batchSize < 1 {
		return uploadPacing{}, fmt.Errorf("upload-batch must be at least 1")
	}
	if delay < 0 {
		return uploadPacing{}, fmt.Errorf("upload-delay must not be negative")
	}
	return uploadPacing{BatchSize: batchSize, Delay: delay}, nil
}

func parseSince(s string, now time.Time) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	if m := relativeSinceRE.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		var t time.Time
		switch m[2] {
		case "d":
			t = now.AddDate(0, 0, -n)
		case "w":
			t = now.AddDate(0, 0, -n*7)
		case "m":
			t = now.AddDate(0, -n, 0)
		case "y":
			t = now.AddDate(-n, 0, 0)
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()), nil
	}
	return time.Time{}, fmt.Errorf("%q (expected YYYY-MM-DD or Nd/Nw/Nm/Ny like 7d, 6m, 1y)", s)
}

func runUploadFit(path string, uploadOpts strava.UploadOptions, pacing uploadPacing) {
	stravaUploader, uploadMethod, err := newConfiguredStravaUploader()
	if err != nil {
		log.Fatalf("Strava upload configuration error: %v", err)
	}
	if err := validateUploadOptionsForMethod(uploadMethod, uploadOpts); err != nil {
		log.Fatalf("Strava upload option error: %v", err)
	}
	files, err := collectUploadFiles(path)
	if err != nil {
		log.Fatalf("Activity file error: %v", err)
	}

	if uploadMethod == stravaUploadMethodAPI {
		log.Println("Refreshing Strava token...")
		if err := stravaUploader.(*strava.Client).RefreshToken(configPath); err != nil {
			log.Fatalf("Strava token refresh error: %v", err)
		}
	} else {
		log.Println("Checking Strava web upload session...")
		if err := stravaUploader.(*strava.WebClient).Check(); err != nil {
			log.Fatalf("Strava web upload session error: %v", err)
		}
	}

	var items []uploadItem
	for _, filePath := range files {
		items = append(items, uploadItem{
			Path:       filePath,
			ExternalID: strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)),
		})
	}
	uploaded, err := uploadActivityItems(stravaUploader, uploadMethod, items, uploadOpts, pacing, nil)
	if err != nil {
		log.Fatalf("Error uploading to Strava: %v", err)
	}
	log.Printf("Upload submitted to Strava. %d file(s) queued.", uploaded)
}

func runSync(since time.Time, uploadOpts strava.UploadOptions, pacing uploadPacing) {
	onelapClient := onelap.NewClient()
	stravaUploader, uploadMethod, err := newConfiguredStravaUploader()
	if err != nil {
		log.Fatalf("Strava upload configuration error: %v", err)
	}
	if err := validateUploadOptionsForMethod(uploadMethod, uploadOpts); err != nil {
		log.Fatalf("Strava upload option error: %v", err)
	}

	// 1. Login to Onelap
	log.Println("Logging in to Onelap...")
	if err := onelapClient.Login(config.GlobalConfig.Onelap.Account, config.GlobalConfig.Onelap.Password); err != nil {
		log.Fatalf("Onelap login error: %v", err)
	}

	// 2. Get activities
	var activities []onelap.Activity
	if since.IsZero() {
		log.Println("Fetching today's activities from Onelap...")
		activities, err = onelapClient.GetTodayActivities()
	} else {
		log.Printf("Fetching activities since %s from Onelap...", since.Format("2006-01-02 15:04:05"))
		activities, err = onelapClient.GetActivitiesSince(since)
	}
	if err != nil {
		log.Fatalf("Error getting activities: %v", err)
	}

	if len(activities) == 0 {
		log.Println("No activities found for today.")
		return
	}

	log.Printf("Found %d activities to check.", len(activities))

	// 3. Prepare Strava upload
	if uploadMethod == stravaUploadMethodAPI {
		log.Println("Refreshing Strava token...")
		if err := stravaUploader.(*strava.Client).RefreshToken(configPath); err != nil {
			log.Fatalf("Strava token refresh error: %v", err)
		}
	} else {
		log.Println("Checking Strava web upload session...")
		if err := stravaUploader.(*strava.WebClient).Check(); err != nil {
			log.Fatalf("Strava web upload session error: %v", err)
		}
	}

	// 4. Download
	tmpDir := "tmp"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		log.Fatalf("Error creating tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var pending []uploadItem
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

		if config.GlobalConfig.ConvertGCJToWGS {
			log.Printf("Converting coordinates from GCJ-02 to WGS-84...")
			if err := fitconv.ConvertFile(fitPath); err != nil {
				log.Printf("Error converting FIT for activity %s: %v", idStr, err)
				continue
			}
		}

		pending = append(pending, uploadItem{Path: fitPath, ExternalID: idStr})
	}

	syncedCount, err := uploadActivityItems(stravaUploader, uploadMethod, pending, uploadOpts, pacing, func(items []uploadItem) {
		for _, item := range items {
			log.Printf("Successfully synced activity %s", item.ExternalID)
			config.AddSyncedID(item.ExternalID)
		}
		if err := config.SaveState(statePath); err != nil {
			log.Printf("Warning: failed to save state: %v", err)
		}
	})
	if err != nil {
		log.Printf("Stopped uploading to Strava: %v", err)
	}
	log.Printf("Sync complete. %d new activities synced.", syncedCount)
}

func collectUploadFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".fit") {
			return nil, fmt.Errorf("only FIT files are supported: %s", path)
		}
		return []string{path}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".fit") {
			files = append(files, filepath.Join(path, entry.Name()))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no FIT files found in %s", path)
	}
	return files, nil
}

func uploadActivityItems(uploader stravaUploader, method stravaUploadMethod, items []uploadItem, opts strava.UploadOptions, pacing uploadPacing, onSuccess func([]uploadItem)) (int, error) {
	success := 0
	for start := 0; start < len(items); start += pacing.BatchSize {
		end := start + pacing.BatchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]
		succeeded, err := uploadActivityBatch(uploader, method, batch, opts)
		if len(succeeded) > 0 {
			success += len(succeeded)
			if onSuccess != nil {
				onSuccess(succeeded)
			}
		}
		if err != nil {
			return success, err
		}
		if end < len(items) && pacing.Delay > 0 {
			log.Printf("Uploaded %d/%d queued file(s); waiting %s before next batch...", success, len(items), pacing.Delay)
			time.Sleep(pacing.Delay)
		}
	}
	return success, nil
}

func uploadActivityBatch(uploader stravaUploader, method stravaUploadMethod, batch []uploadItem, opts strava.UploadOptions) ([]uploadItem, error) {
	if method == stravaUploadMethodWeb {
		batchUploader, ok := uploader.(stravaBatchUploader)
		if !ok {
			return nil, fmt.Errorf("configured Strava web uploader does not support batch upload")
		}
		var paths []string
		for _, item := range batch {
			paths = append(paths, item.Path)
		}
		log.Printf("Uploading %d file(s) to Strava...", len(paths))
		if err := batchUploader.UploadActivities(paths, opts); err != nil {
			return nil, err
		}
		return batch, nil
	}

	var succeeded []uploadItem
	var firstErr error
	for _, item := range batch {
		log.Printf("Uploading activity file to Strava: %s", item.Path)
		if err := uploader.UploadActivity(item.Path, item.ExternalID, opts); err != nil {
			log.Printf("Error uploading to Strava: %v", err)
			if firstErr == nil {
				firstErr = err
			}
			if isRateLimitError(err) {
				return succeeded, err
			}
			continue
		}
		succeeded = append(succeeded, item)
	}
	return succeeded, firstErr
}

func isRateLimitError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "rate limit")
}
