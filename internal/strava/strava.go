package strava

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"OnelapSyncStrava/internal/config"
	"github.com/go-resty/resty/v2"
)

const (
	StravaBaseURL = "https://www.strava.com/api/v3"
)

type Client struct {
	restyClient *resty.Client
}

func NewClient() *Client {
	client := resty.New().
		SetTimeout(120 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(5 * time.Second).
		SetRetryMaxWaitTime(20 * time.Second)

	return &Client{
		restyClient: client,
	}
}

func (c *Client) RefreshToken(configPath string) error {
	cfg := &config.GlobalConfig.Strava
	if cfg.AccessToken != "" && time.Now().Unix() < cfg.ExpiresAt {
		return nil // Still valid
	}

	resp, err := c.restyClient.R().
		SetFormData(map[string]string{
			"client_id":     cfg.ClientID,
			"client_secret": cfg.ClientSecret,
			"grant_type":    "refresh_token",
			"refresh_token": cfg.RefreshToken,
		}).
		Post("https://www.strava.com/api/v3/oauth/token")

	if err != nil {
		return fmt.Errorf("failed to refresh strava token: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("refresh token failed with status: %s, body: %s", resp.Status(), resp.String())
	}

	type TokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
	}

	var tokenData TokenResponse
	if err := json.Unmarshal(resp.Body(), &tokenData); err != nil {
		return fmt.Errorf("failed to unmarshal token response: %w", err)
	}

	cfg.AccessToken = tokenData.AccessToken
	cfg.RefreshToken = tokenData.RefreshToken
	cfg.ExpiresAt = tokenData.ExpiresAt

	// Save the updated config back to file
	return config.SaveConfig(configPath)
}

func (c *Client) Check(configPath string) error {
	cfg := &config.GlobalConfig.Strava
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return fmt.Errorf("strava client_id and client_secret must be set")
	}
	if cfg.RefreshToken == "" {
		return fmt.Errorf("strava refresh_token is missing, run auth first")
	}
	// Try refreshing the token as a check
	return c.RefreshToken(configPath)
}

// UploadOptions are runtime-tunable fields forwarded to Strava's /uploads
// endpoint. Empty/false fields are omitted from the request.
type UploadOptions struct {
	Commute     bool
	Trainer     bool
	Name        string
	Description string
}

func (c *Client) UploadActivity(filePath, externalID string, opts UploadOptions) error {
	cfg := &config.GlobalConfig.Strava
	form := map[string]string{
		"data_type":   "fit",
		"external_id": externalID,
	}
	if opts.Commute {
		form["commute"] = "1"
	}
	if opts.Trainer {
		form["trainer"] = "1"
	}
	if opts.Name != "" {
		form["name"] = opts.Name
	}
	if opts.Description != "" {
		form["description"] = opts.Description
	}

	resp, err := c.restyClient.R().
		SetHeader("Authorization", "Bearer "+cfg.AccessToken).
		SetFile("file", filePath).
		SetFormData(form).
		Post(StravaBaseURL + "/uploads")

	if err != nil {
		return fmt.Errorf("failed to upload activity to strava: %w", err)
	}

	if resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusAccepted {
		return fmt.Errorf("upload failed with status: %s, body: %s", resp.Status(), resp.String())
	}

	return nil
}
