package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type OnelapConfig struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type StravaConfig struct {
	ClientID        string `json:"client_id"`
	ClientSecret    string `json:"client_secret"`
	AccessToken     string `json:"access_token"`
	RefreshToken    string `json:"refresh_token"`
	ExpiresAt       int64  `json:"expires_at"` // Unix timestamp
	UploadMethod    string `json:"upload_method"`
	WebCookieHeader string `json:"web_cookie_header"`
}

type Config struct {
	Onelap          OnelapConfig `json:"onelap"`
	Strava          StravaConfig `json:"strava"`
	ConvertGCJToWGS bool         `json:"convert_gcj_to_wgs"`
}

var GlobalConfig Config

func LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &GlobalConfig); err != nil {
			return fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}

	// Override with environment variables
	if v := os.Getenv("ONELAP_ACCOUNT"); v != "" {
		GlobalConfig.Onelap.Account = v
	}
	if v := os.Getenv("ONELAP_PASSWORD"); v != "" {
		GlobalConfig.Onelap.Password = v
	}
	if v := os.Getenv("STRAVA_CLIENT_ID"); v != "" {
		GlobalConfig.Strava.ClientID = v
	}
	if v := os.Getenv("STRAVA_CLIENT_SECRET"); v != "" {
		GlobalConfig.Strava.ClientSecret = v
	}
	if v := os.Getenv("STRAVA_UPLOAD_METHOD"); v != "" {
		GlobalConfig.Strava.UploadMethod = v
	}
	if v := os.Getenv("STRAVA_WEB_COOKIE_HEADER"); v != "" {
		GlobalConfig.Strava.WebCookieHeader = v
	}

	return nil
}

func SaveConfig(path string) error {
	data, err := json.MarshalIndent(GlobalConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
