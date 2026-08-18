package minecraft

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	manifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	ttl         = 24 * time.Hour
	limit       = 60
)

var fallback = []string{
	"1.21.4", "1.21.1", "1.21", "1.20.6", "1.20.4", "1.20.1",
	"1.19.4", "1.19.2", "1.18.2", "1.17.1", "1.16.5", "1.12.2", "1.7.10",
}

type Catalog struct {
	mu       sync.Mutex
	versions []string
	source   string
	fetched  time.Time
	http     *http.Client
}

func New() *Catalog {
	return &Catalog{http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Catalog) Warm() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	versions, source := c.Versions(ctx)
	log.Printf("minecraft: %d versions (%s)", len(versions), source)
}

func (c *Catalog) Versions(ctx context.Context) ([]string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.versions != nil && time.Since(c.fetched) < ttl {
		return c.versions, c.source
	}

	list, err := c.fetch(ctx)
	if err != nil {
		if c.versions != nil {
			return c.versions, c.source
		}
		c.versions, c.source, c.fetched = fallback, "fallback", time.Now()
		return c.versions, c.source
	}
	c.versions, c.source, c.fetched = list, "mojang", time.Now()
	return c.versions, c.source
}

func (c *Catalog) fetch(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errStatus(resp.StatusCode)
	}

	var body struct {
		Versions []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	out := make([]string, 0, limit)
	for _, v := range body.Versions {
		if v.Type != "release" {
			continue
		}
		out = append(out, v.ID)
		if len(out) == limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, errEmpty
	}
	return out, nil
}

type versionsError string

func (e versionsError) Error() string { return string(e) }

const errEmpty = versionsError("mojang manifest has no releases")

func errStatus(code int) error {
	return versionsError("mojang manifest: " + http.StatusText(code))
}
