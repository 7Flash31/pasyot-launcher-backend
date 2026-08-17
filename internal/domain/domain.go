package domain

import "time"

type User struct {
	ID        string    `json:"id"`
	VedrowSub string    `json:"vedrow_sub"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

type Modpack struct {
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	LatestVersion int       `json:"latest_version"`
	CreatedAt     time.Time `json:"created_at"`
}

type Version struct {
	Version    int       `json:"version"`
	Notes      string    `json:"notes,omitempty"`
	FileCount  int       `json:"file_count"`
	TotalBytes int64     `json:"total_bytes"`
	CreatedBy  string    `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type File struct {
	Path     string `json:"path"`
	Group    string `json:"group"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Optional bool   `json:"optional"`
	URL      string `json:"url,omitempty"`
}

type Manifest struct {
	Format     int       `json:"format"`
	Modpack    string    `json:"modpack"`
	Name       string    `json:"name"`
	Version    int       `json:"version"`
	Notes      string    `json:"notes,omitempty"`
	Groups     []Group   `json:"groups"`
	FileCount  int       `json:"file_count"`
	TotalBytes int64     `json:"total_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	Files      []File    `json:"files"`
}

type Group struct {
	Name     string `json:"name"`
	Optional bool   `json:"optional"`
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
}

type Pack struct {
	Format      int    `json:"format"`
	Server      string `json:"server"`
	Modpack     string `json:"modpack"`
	Name        string `json:"name"`
	Version     int    `json:"version"`
	ManifestURL string `json:"manifest_url"`
}

type LauncherBuild struct {
	Version   string     `json:"version"`
	Filename  string     `json:"filename"`
	Size      int64      `json:"size,omitempty"`
	SHA256    string     `json:"sha256,omitempty"`
	URL       string     `json:"url,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}
