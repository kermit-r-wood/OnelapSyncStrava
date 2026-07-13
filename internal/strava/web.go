package strava

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	StravaWebBaseURL = "https://www.strava.com"
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:150.0) Gecko/20100101 Firefox/150.0"
)

type WebOptions struct {
	BaseURL      string
	CookieHeader string
}

type WebClient struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
}

func NewWebClient(opts WebOptions) (*WebClient, error) {
	base := opts.BaseURL
	if base == "" {
		base = StravaWebBaseURL
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("parse Strava web base URL: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("Strava web base URL must include scheme and host")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	if opts.CookieHeader != "" {
		if ok, err := loadCookieHeader(jar, baseURL, opts.CookieHeader); err != nil {
			return nil, err
		} else if !ok {
			return nil, fmt.Errorf("STRAVA_WEB_COOKIE_HEADER must contain a Cookie header or cookie pairs")
		}
	}

	return &WebClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 120 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		userAgent: defaultUserAgent,
	}, nil
}

func (c *WebClient) Check() error {
	token, err := c.fetchCSRFToken()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("Strava web CSRF token is empty")
	}
	return nil
}

func (c *WebClient) UploadActivity(filePath, externalID string, opts UploadOptions) error {
	_ = externalID
	return c.UploadActivities([]string{filePath}, opts)
}

func (c *WebClient) UploadActivities(filePaths []string, opts UploadOptions) error {
	if opts.Commute || opts.Trainer || opts.Name != "" || opts.Description != "" {
		return fmt.Errorf("commute, trainer, name and description are not supported by Strava web upload")
	}
	if len(filePaths) == 0 {
		return nil
	}

	token, err := c.fetchCSRFToken()
	if err != nil {
		return err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("_method", "post"); err != nil {
		return fmt.Errorf("write _method field: %w", err)
	}
	if err := writer.WriteField("authenticity_token", token); err != nil {
		return fmt.Errorf("write authenticity_token field: %w", err)
	}
	for _, filePath := range filePaths {
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open activity file: %w", err)
		}
		part, err := writer.CreateFormFile("files[]", filepath.Base(filePath))
		if err != nil {
			file.Close()
			return fmt.Errorf("create files[] field: %w", err)
		}
		if _, err := io.Copy(part, file); err != nil {
			file.Close()
			return fmt.Errorf("write activity file field: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close activity file: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.resolve("/upload/files"), &body)
	if err != nil {
		return fmt.Errorf("create Strava web upload request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/plain, */*; q=0.01")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", c.origin())
	req.Header.Set("Referer", c.resolve("/upload/select"))
	req.Header.Set("X-CSRF-Token", token)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload activity via Strava web: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		location := resp.Header.Get("Location")
		return fmt.Errorf("Strava web upload redirected to %s; cookies may be missing or expired", location)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Strava web upload failed with status %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

func (c *WebClient) fetchCSRFToken() (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.resolve("/upload/select"), nil)
	if err != nil {
		return "", fmt.Errorf("create Strava upload select request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch Strava upload select page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		location := resp.Header.Get("Location")
		if strings.Contains(location, "/login") {
			return "", fmt.Errorf("Strava web cookies are missing or expired; upload/select redirected to login")
		}
		return "", fmt.Errorf("Strava upload select redirected to %s", location)
	}
	if strings.Contains(resp.Request.URL.Path, "/login") {
		return "", fmt.Errorf("Strava web cookies are missing or expired; upload/select redirected to login")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Strava upload select failed with status %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("read Strava upload select page: %w", err)
	}
	token, err := ExtractCSRFToken(string(data))
	if err != nil {
		return "", fmt.Errorf("extract Strava web CSRF token from upload/select: %w", err)
	}
	return token, nil
}

func ExtractCSRFToken(pageHTML string) (string, error) {
	doc, err := html.Parse(strings.NewReader(pageHTML))
	if err != nil {
		return "", fmt.Errorf("parse HTML: %w", err)
	}

	var walk func(*html.Node) string
	walk = func(n *html.Node) string {
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "meta":
				if attr(n, "name") == "csrf-token" {
					if token := attr(n, "content"); token != "" {
						return token
					}
				}
			case "input":
				if attr(n, "name") == "authenticity_token" {
					if token := attr(n, "value"); token != "" {
						return token
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if token := walk(child); token != "" {
				return token
			}
		}
		return ""
	}

	if token := walk(doc); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("csrf-token meta tag or authenticity_token input not found")
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func (c *WebClient) resolve(path string) string {
	u := *c.baseURL
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (c *WebClient) origin() string {
	return c.baseURL.Scheme + "://" + c.baseURL.Host
}

func loadCookieHeader(jar http.CookieJar, baseURL *url.URL, data string) (bool, error) {
	line := firstCookieHeaderLine(data)
	if line == "" {
		return false, nil
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(strings.ToLower(line), "cookie:") {
		line = strings.TrimSpace(line[len("cookie:"):])
	}
	if line == "" {
		return true, fmt.Errorf("cookie header file is empty")
	}

	var cookies []*http.Cookie
	for _, part := range strings.Split(line, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			return true, fmt.Errorf("parse cookie header: cookie %q is missing '='", part)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			return true, fmt.Errorf("parse cookie header: empty cookie name")
		}
		cookies = append(cookies, &http.Cookie{
			Name:  name,
			Value: strings.TrimSpace(value),
			Path:  "/",
		})
	}
	if len(cookies) == 0 {
		return true, fmt.Errorf("cookie header file did not contain any cookies")
	}
	jar.SetCookies(baseURL, cookies)
	return true, nil
}

func firstCookieHeaderLine(data string) string {
	for _, rawLine := range strings.Split(data, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "cookie:") || (strings.Contains(line, "=") && strings.Contains(line, ";")) {
			return line
		}
		return ""
	}
	return ""
}
