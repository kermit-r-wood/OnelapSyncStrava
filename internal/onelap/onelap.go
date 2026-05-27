package onelap

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	OnelapSecret      = "fe9f8382418fcdeb136461cac6acae7b"
	LoginBaseURL      = "https://www.onelap.cn/api"
	RideRecordBaseURL = "https://otm.onelap.cn/api/otm/ride_record"
)

type Client struct {
	restyClient *resty.Client
	UID         string
	// Token is the JWT used in the Authorization header for subsequent API calls.
	Token string
}

func NewClient() *Client {
	client := resty.New().
		SetTimeout(30 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(2 * time.Second).
		SetRetryMaxWaitTime(10 * time.Second)

	return &Client{
		restyClient: client,
	}
}

func md5Hex(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func randomHex(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = hexChars[rand.Intn(len(hexChars))]
	}
	return string(b)
}

func (c *Client) Login(account, password string) error {
	if account == "" || password == "" {
		return fmt.Errorf("onelap account and password cannot be empty")
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randomHex(16)
	passwordMd5 := md5Hex(password)

	// Signature calculation matching Onelap's verification
	signStr := fmt.Sprintf("account=%s&nonce=%s&password=%s&timestamp=%s&key=%s", account, nonce, passwordMd5, timestamp, OnelapSecret)
	sign := md5Hex(signStr)

	body := fmt.Sprintf(`{"account":"%s","password":"%s"}`, account, passwordMd5)

	resp, err := c.restyClient.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("nonce", nonce).
		SetHeader("timestamp", timestamp).
		SetHeader("sign", sign).
		SetBody(body).
		Post(LoginBaseURL + "/login")

	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("login failed with status: %s, body: %s", resp.Status(), resp.String())
	}

	type LoginResponse struct {
		Data []struct {
			Token        string `json:"token"`
			RefreshToken string `json:"refresh_token"`
			UserInfo     struct {
				UID json.Number `json:"uid"` // Use json.Number to avoid scientific notation
			} `json:"userinfo"`
		} `json:"data"`
	}

	var loginData LoginResponse
	if err := json.Unmarshal(resp.Body(), &loginData); err != nil {
		return fmt.Errorf("failed to unmarshal login response: %w", err)
	}

	if len(loginData.Data) == 0 {
		return fmt.Errorf("invalid login response: no data")
	}

	c.UID = loginData.Data[0].UserInfo.UID.String()
	// The token from login is used as the Authorization header for ride record APIs.
	c.Token = loginData.Data[0].Token

	return nil
}

func (c *Client) Check(account, password string) error {
	return c.Login(account, password)
}

// Activity represents a single ride record from the Onelap list API.
// POST https://u.onelap.cn/api/otm/ride_record/list
type Activity struct {
	ExternalID  string  `json:"id"`               // Unique activity ID
	StartTime   string  `json:"start_riding_time"` // Format: "2026-05-07 21:30:51"
	DistanceKm  float64 `json:"distance_km"`
	TimeSeconds int     `json:"time_seconds"`
	// DURL is populated by GetDownloadURL, not present in the list response.
	DURL string `json:"-"`
}

// rideListRequest is the request body for the ride record list API.
type rideListRequest struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

// rideListResponse is the response structure for the ride record list API.
type rideListResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		List       []Activity `json:"list"`
		Pagination struct {
			TotalPages  int  `json:"total_pages"`
			HasMore     bool `json:"has_more"`
			CurrentPage int  `json:"current_page"`
			PerPage     int  `json:"per_page"`
			Total       int  `json:"total"`
		} `json:"pagination"`
	} `json:"data"`
}

// authRequest returns a resty request pre-configured with the auth headers.
func (c *Client) authRequest() *resty.Request {
	return c.restyClient.R().
		SetHeader("Authorization", c.Token).
		SetCookie(&http.Cookie{Name: "ouid", Value: c.UID})
}

// GetRecentActivities fetches the most recent ride records, up to the given number of pages.
// Activities are returned in reverse chronological order (newest first).
func (c *Client) GetRecentActivities(pages int) ([]Activity, error) {
	const pageSize = 20
	var all []Activity

	for page := 1; page <= pages; page++ {
		reqBody := rideListRequest{Page: page, Limit: pageSize}
		var result rideListResponse

		resp, err := c.authRequest().
			SetHeader("Content-Type", "application/json").
			SetBody(reqBody).
			SetResult(&result).
			Post(RideRecordBaseURL + "/list")

		if err != nil {
			return nil, fmt.Errorf("get activity list (page %d) failed: %w", page, err)
		}

		if resp.StatusCode() != http.StatusOK {
			return nil, fmt.Errorf("get activity list failed with status: %s, body: %s", resp.Status(), resp.String())
		}

		if result.Code != 200 {
			return nil, fmt.Errorf("get activity list API error: code=%d, message=%s", result.Code, result.Message)
		}

		all = append(all, result.Data.List...)

		if !result.Data.Pagination.HasMore {
			break
		}
	}

	return all, nil
}

// GetActivities fetches all ride records by paginating through all pages.
func (c *Client) GetActivities() ([]Activity, error) {
	const maxPages = 9999
	return c.GetRecentActivities(maxPages)
}

// GetActivitiesSince fetches all ride records with start_riding_time on or after `since`.
// Records are returned newest-first, so pagination short-circuits once an older record appears.
func (c *Client) GetActivitiesSince(since time.Time) ([]Activity, error) {
	const pageSize = 20
	var matched []Activity

	for page := 1; ; page++ {
		reqBody := rideListRequest{Page: page, Limit: pageSize}
		var result rideListResponse

		resp, err := c.authRequest().
			SetHeader("Content-Type", "application/json").
			SetBody(reqBody).
			SetResult(&result).
			Post(RideRecordBaseURL + "/list")

		if err != nil {
			return nil, fmt.Errorf("get activity list (page %d) failed: %w", page, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, fmt.Errorf("get activity list failed with status: %s, body: %s", resp.Status(), resp.String())
		}
		if result.Code != 200 {
			return nil, fmt.Errorf("get activity list API error: code=%d, message=%s", result.Code, result.Message)
		}

		stop := false
		for _, act := range result.Data.List {
			t, err := time.ParseInLocation("2006-01-02 15:04:05", act.StartTime, time.Local)
			if err != nil {
				continue
			}
			if t.Before(since) {
				stop = true
				break
			}
			matched = append(matched, act)
		}

		if stop || !result.Data.Pagination.HasMore {
			break
		}
	}

	return matched, nil
}

func (c *Client) GetTodayActivities() ([]Activity, error) {
	// Activities are returned newest-first, so today's records are on page 1.
	// Fetch up to 2 pages (40 activities) to handle timezone edge cases.
	all, err := c.GetRecentActivities(2)
	if err != nil {
		return nil, err
	}

	// Filter for today and yesterday (to handle midnight timezone differences).
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().Add(-24 * time.Hour).Format("2006-01-02")

	var todayActivities []Activity
	for _, act := range all {
		// start_riding_time format: "2026-05-07 21:30:51"
		if len(act.StartTime) >= 10 {
			dateStr := act.StartTime[:10]
			if dateStr == today || dateStr == yesterday {
				todayActivities = append(todayActivities, act)
			}
		}
	}

	return todayActivities, nil
}

// analysisResponse is the response from GET /api/otm/ride_record/analysis/{id}.
type analysisResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		RidingRecord struct {
			// FileKey is the object-storage path of the FIT file
			// (e.g. "geo/20260527/Magene_C706_..._.fit"). It is base64-encoded
			// and used as the path segment for the fit_content endpoint.
			FileKey string `json:"fileKey"`
		} `json:"ridingRecord"`
	} `json:"data"`
}

// GetDownloadURL builds the authenticated FIT download URL for a specific activity.
// It calls GET /api/otm/ride_record/analysis/{id} to obtain the fileKey, then
// returns the fit_content endpoint URL with the base64-encoded fileKey as path.
// Unlike the previous pre-signed durl, this URL does not expire.
func (c *Client) GetDownloadURL(activityID string) (string, error) {
	var result analysisResponse

	resp, err := c.authRequest().
		SetResult(&result).
		Get(fmt.Sprintf("%s/analysis/%s", RideRecordBaseURL, activityID))

	if err != nil {
		return "", fmt.Errorf("get activity analysis for %s failed: %w", activityID, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("get activity analysis failed with status: %s", resp.Status())
	}

	if result.Code != 200 {
		return "", fmt.Errorf("get activity analysis API error: code=%d, message=%s", result.Code, result.Message)
	}

	fileKey := result.Data.RidingRecord.FileKey
	if fileKey == "" {
		return "", fmt.Errorf("activity %s has no fileKey", activityID)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(fileKey))
	return fmt.Sprintf("%s/analysis/fit_content/%s", RideRecordBaseURL, encoded), nil
}

// DownloadFIT downloads the FIT file from the given URL to destPath.
// The URL is the authenticated fit_content endpoint, so the request must
// carry the same Authorization header / ouid cookie as other ride record APIs.
func (c *Client) DownloadFIT(durl, destPath string) error {
	resp, err := c.authRequest().
		SetOutput(destPath).
		Get(durl)

	if err != nil {
		return fmt.Errorf("failed to download FIT file: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status())
	}

	return nil
}
