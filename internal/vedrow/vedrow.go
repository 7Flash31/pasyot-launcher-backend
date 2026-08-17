package vedrow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	apiURL      string
	webURL      string
	clientID    string
	secret      string
	redirectURI string
	http        *http.Client
}

func New(apiURL, webURL, clientID, secret, redirectURI string) *Client {
	return &Client{
		apiURL:      strings.TrimRight(apiURL, "/"),
		webURL:      strings.TrimRight(webURL, "/"),
		clientID:    clientID,
		secret:      secret,
		redirectURI: redirectURI,
		http:        &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Configured() bool {
	return c.clientID != "" && c.secret != "" && c.apiURL != "" && c.webURL != ""
}

func (c *Client) RedirectURI() string { return c.redirectURI }

func NewVerifier() string { return randomURLSafe(32) }

func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *Client) AuthorizeURL(state, verifier string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", c.clientID)
	q.Set("redirect_uri", c.redirectURI)
	q.Set("scope", "openid profile email")
	q.Set("state", state)
	q.Set("code_challenge", Challenge(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("nonce", randomURLSafe(16))
	return c.webURL + "/oauth/authorize?" + q.Encode()
}

type UserInfo struct {
	Sub               string `json:"sub"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Picture           string `json:"picture"`
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.redirectURI)
	form.Set("code_verifier", verifier)
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/oauth/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("vedrow unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return "", fmt.Errorf("code exchange: %s (%s)", e.Error, e.Description)
		}
		return "", fmt.Errorf("code exchange: %s", resp.Status)
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("code exchange: malformed response: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("code exchange: empty access_token")
	}
	return out.AccessToken, nil
}

func (c *Client) UserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+"/oauth/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vedrow unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo: %s", resp.Status)
	}
	var info UserInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return nil, fmt.Errorf("userinfo: malformed response: %w", err)
	}
	if info.Sub == "" {
		return nil, errors.New("userinfo: missing sub")
	}
	return &info, nil
}

func (c *Client) Login(ctx context.Context, code, verifier string) (*UserInfo, error) {
	token, err := c.Exchange(ctx, code, verifier)
	if err != nil {
		return nil, err
	}
	return c.UserInfo(ctx, token)
}

func NewState() string { return randomURLSafe(24) }

func randomURLSafe(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
