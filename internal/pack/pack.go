package pack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"pasyot-launcher/internal/blob"
	"pasyot-launcher/internal/domain"
)

const (
	defaultMaxFiles = 100_000
	defaultMaxBytes = 32 << 30
)

var knownGroups = map[string]bool{
	"mods": true, "config": true, "defaultconfigs": true, "saves": true,
	"resourcepacks": true, "shaderpacks": true, "scripts": true, "kubejs": true,
	"libraries": true, "versions": true, "assets": true, "journeymap": true,
	"schematics": true, "emotes": true, "patchouli_books": true, "local": true,
	"XaeroWaypoints": true, "XaeroWorldMap": true, "screenshots": true,
}

type Options struct {
	Include  []string
	Optional []string
	MaxFiles int
	MaxBytes int64
}

func Extract(zipPath string, blobs *blob.Store, opts Options) ([]domain.File, error) {
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultMaxFiles
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultMaxBytes
	}
	include := set(opts.Include)
	optional := set(opts.Optional)

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("not a valid zip: %w", err)
	}
	defer zr.Close()

	type entry struct {
		file *zip.File
		name string
	}
	entries := make([]entry, 0, len(zr.File))
	for _, f := range zr.File {
		name, ok := cleanName(f.Name)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		entries = append(entries, entry{file: f, name: name})
	}
	if len(entries) == 0 {
		return nil, errors.New("archive has no files")
	}
	if len(entries) > opts.MaxFiles {
		return nil, fmt.Errorf("too many files in archive: %d (limit %d)", len(entries), opts.MaxFiles)
	}

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	if prefix := wrapperDir(names); prefix != "" {
		for i := range entries {
			entries[i].name = strings.TrimPrefix(entries[i].name, prefix+"/")
		}
	}

	files := make([]domain.File, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	var total int64
	for _, e := range entries {
		group := topDir(e.name)
		if len(include) > 0 && !include[group] {
			continue
		}
		if seen[e.name] {
			continue
		}
		seen[e.name] = true

		total += int64(e.file.UncompressedSize64)
		if total > opts.MaxBytes {
			return nil, fmt.Errorf("uncompressed size exceeds the limit of %d bytes", opts.MaxBytes)
		}

		rc, err := e.file.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.name, err)
		}
		sha, size, err := blobs.Put(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.name, err)
		}
		files = append(files, domain.File{
			Path: e.name, Group: group, Size: size, SHA256: sha,
			Optional: optional[group],
		})
	}
	if len(files) == 0 {
		return nil, errors.New("no files left after the include filter")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func Fingerprint(files []domain.File) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%s\x00%d\n", f.Path, f.SHA256, boolByte(f.Optional))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func Groups(files []domain.File) []domain.Group {
	order := []string{}
	byName := map[string]*domain.Group{}
	for _, f := range files {
		g, ok := byName[f.Group]
		if !ok {
			g = &domain.Group{Name: f.Group, Optional: f.Optional}
			byName[f.Group] = g
			order = append(order, f.Group)
		}
		g.Files++
		g.Bytes += f.Size
	}
	out := make([]domain.Group, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cleanName(name string) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return "", false
	}
	if len(name) > 1 && name[1] == ':' {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	for _, part := range strings.Split(clean, "/") {
		switch part {
		case "__MACOSX", ".DS_Store", "Thumbs.db", "desktop.ini":
			return "", false
		}
	}
	if strings.HasPrefix(path.Base(clean), "._") {
		return "", false
	}
	return clean, true
}

func wrapperDir(names []string) string {
	top := topDir(names[0])
	if top == "" || knownGroups[top] {
		return ""
	}
	for _, name := range names {
		if topDir(name) != top {
			return ""
		}
	}
	return top
}

func topDir(name string) string {
	if i := strings.IndexByte(name, '/'); i > 0 {
		return name[:i]
	}
	return ""
}

func set(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	m := make(map[string]bool, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			m[v] = true
		}
	}
	return m
}

func boolByte(b bool) int {
	if b {
		return 1
	}
	return 0
}
