package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"MageneSync/internal/config"
)

// Authorize starts a local HTTP server, opens the browser for Strava OAuth,
// and exchanges the authorization code for tokens. Tokens are saved to config.
func Authorize(configPath string) error {
	cfg := &config.GlobalConfig.Strava
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return fmt.Errorf("strava client_id and client_secret must be set in config.json before authorization")
	}

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to find available port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// Start local HTTP server to receive callback
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no authorization code received"
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<html><body><h2>Authorization Failed</h2><p>%s</p><p>You can close this window.</p></body></html>", errMsg)
			errCh <- fmt.Errorf("authorization failed: %s", errMsg)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body><h2>Authorization Successful!</h2><p>You can close this window and return to the terminal.</p></body></html>")
		codeCh <- code
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("failed to start callback server: %w", err)
		}
	}()

	// Build authorization URL
	authURL := fmt.Sprintf(
		"https://www.strava.com/oauth/authorize?client_id=%s&response_type=code&redirect_uri=%s&approval_prompt=force&scope=read,activity:write",
		cfg.ClientID,
		redirectURI,
	)

	log.Printf("Please visit the following URL to authorize the application:\n\n%s\n", authURL)
	log.Println("Waiting for authorization callback...")

	// Wait for callback or timeout
	var code string
	select {
	case code = <-codeCh:
		// success
	case err := <-errCh:
		server.Shutdown(context.Background())
		return err
	case <-time.After(5 * time.Minute):
		server.Shutdown(context.Background())
		return fmt.Errorf("authorization timed out after 5 minutes")
	}

	server.Shutdown(context.Background())

	// Exchange authorization code for tokens
	log.Println("Exchanging authorization code for tokens...")
	client := resty.New().SetTimeout(30 * time.Second)
	resp, err := client.R().
		SetFormData(map[string]string{
			"client_id":     cfg.ClientID,
			"client_secret": cfg.ClientSecret,
			"code":          code,
			"grant_type":    "authorization_code",
		}).
		Post("https://www.strava.com/api/v3/oauth/token")

	if err != nil {
		return fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("token exchange failed with status %s: %s", resp.Status(), resp.String())
	}

	type TokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
	}

	var tokenData TokenResponse
	if err := json.Unmarshal(resp.Body(), &tokenData); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	cfg.AccessToken = tokenData.AccessToken
	cfg.RefreshToken = tokenData.RefreshToken
	cfg.ExpiresAt = tokenData.ExpiresAt

	if err := config.SaveConfig(configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Println("Strava authorization successful! Tokens saved to config.")
	return nil
}


