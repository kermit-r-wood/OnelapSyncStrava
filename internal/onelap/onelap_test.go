package onelap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// rewriteTransport redirects all outbound requests to a test server while
// preserving the original path and body, so production code can hit its
// real RideRecordBaseURL constant unchanged.
type rewriteTransport struct {
	base *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newMockClient(srv *httptest.Server) *Client {
	target, _ := url.Parse(srv.URL)
	c := NewClient()
	c.restyClient.SetTransport(&rewriteTransport{base: target}).SetRetryCount(0)
	c.Token = "test-token"
	c.UID = "12345"
	return c
}

func listResponseBody(t *testing.T, activities []Activity, hasMore bool) []byte {
	t.Helper()
	body := map[string]any{
		"code":    200,
		"message": "ok",
		"data": map[string]any{
			"list": activities,
			"pagination": map[string]any{
				"has_more": hasMore,
			},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal list response: %v", err)
	}
	return b
}

// TestGetActivitiesSince_ShortCircuitsOnOlderRecord verifies pagination stops
// as soon as a record older than `since` appears, since the API returns
// records newest-first.
func TestGetActivitiesSince_ShortCircuitsOnOlderRecord(t *testing.T) {
	pages := [][]byte{
		listResponseBody(t, []Activity{
			{ExternalID: "a1", StartTime: "2026-05-10 08:00:00"},
			{ExternalID: "a2", StartTime: "2026-05-05 08:00:00"},
		}, true),
		listResponseBody(t, []Activity{
			{ExternalID: "a3", StartTime: "2026-05-03 08:00:00"},
			{ExternalID: "a4", StartTime: "2026-04-28 08:00:00"},
		}, true),
		listResponseBody(t, []Activity{
			{ExternalID: "a5", StartTime: "2026-04-01 08:00:00"},
		}, false),
	}
	var pageRequests []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rideListRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		pageRequests = append(pageRequests, req.Page)
		if req.Page < 1 || req.Page > len(pages) {
			http.Error(w, "unexpected page", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pages[req.Page-1])
	}))
	defer srv.Close()

	c := newMockClient(srv)
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	got, err := c.GetActivitiesSince(since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantIDs := []string{"a1", "a2", "a3"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d activities, want %d: %+v", len(got), len(wantIDs), got)
	}
	for i, id := range wantIDs {
		if got[i].ExternalID != id {
			t.Errorf("activity[%d].ID = %q, want %q", i, got[i].ExternalID, id)
		}
	}
	if want := []int{1, 2}; !equalInts(pageRequests, want) {
		t.Errorf("fetched pages = %v, want %v (should stop after older record on page 2)", pageRequests, want)
	}
}

// TestGetActivitiesSince_StopsWhenHasMoreFalse verifies pagination stops when
// the API signals no more pages, even if no older record was seen.
func TestGetActivitiesSince_StopsWhenHasMoreFalse(t *testing.T) {
	var pageRequests []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rideListRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		pageRequests = append(pageRequests, req.Page)
		body := listResponseBody(t, []Activity{
			{ExternalID: "a1", StartTime: "2026-05-10 08:00:00"},
			{ExternalID: "a2", StartTime: "2026-05-05 08:00:00"},
		}, false)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newMockClient(srv)
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	got, err := c.GetActivitiesSince(since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d activities, want 2: %+v", len(got), got)
	}
	if want := []int{1}; !equalInts(pageRequests, want) {
		t.Errorf("fetched pages = %v, want %v", pageRequests, want)
	}
}

// TestGetActivitiesSince_BoundaryIncludesEqual verifies a record whose start
// time equals `since` is included (the filter is strictly Before, not <=).
func TestGetActivitiesSince_BoundaryIncludesEqual(t *testing.T) {
	body := listResponseBody(t, []Activity{
		{ExternalID: "exact", StartTime: "2026-05-01 00:00:00"},
	}, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newMockClient(srv)
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	got, err := c.GetActivitiesSince(since)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ExternalID != "exact" {
		t.Fatalf("got %+v, want one activity 'exact'", got)
	}
}

// TestGetActivitiesSince_APIError verifies an API-level error code surfaces
// as a returned error rather than silently dropping records.
func TestGetActivitiesSince_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":401,"message":"unauthorized"}`))
	}))
	defer srv.Close()

	c := newMockClient(srv)
	_, err := c.GetActivitiesSince(time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local))
	if err == nil {
		t.Fatal("expected error for API code=401, got nil")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
