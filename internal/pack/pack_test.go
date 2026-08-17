package pack

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"pasyot-launcher/internal/blob"
)

func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pack.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func blobs(t *testing.T) *blob.Store {
	t.Helper()
	s, err := blob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func extract(t *testing.T, zipPath string, opts Options) []string {
	t.Helper()
	files, err := Extract(zipPath, blobs(t), opts)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func TestExtractKeepsGameFolders(t *testing.T) {
	z := makeZip(t, map[string]string{
		"mods/a.jar":            "a",
		"config/foo.toml":       "foo",
		"saves/world/level.dat": "lvl",
		"options.txt":           "opts",
	})
	got := extract(t, z, Options{})
	want := []string{"config/foo.toml", "mods/a.jar", "options.txt", "saves/world/level.dat"}
	if len(got) != len(want) {
		t.Fatalf("got %d files, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractRejectsEscapingPaths(t *testing.T) {
	z := makeZip(t, map[string]string{
		"mods/ok.jar":            "ok",
		"../../../../etc/passwd": "pwned",
		"/absolute/evil":         "pwned",
		"mods/../../outside.txt": "pwned",
		"__MACOSX/mods/._ok.jar": "junk",
		"mods/.DS_Store":         "junk",
	})
	got := extract(t, z, Options{})
	if len(got) != 1 || got[0] != "mods/ok.jar" {
		t.Fatalf("only mods/ok.jar should survive, got %v", got)
	}
}

func TestExtractStripsWrapperDir(t *testing.T) {
	z := makeZip(t, map[string]string{
		"MyPack/mods/a.jar":      "a",
		"MyPack/config/foo.toml": "foo",
	})
	got := extract(t, z, Options{})
	want := map[string]bool{"config/foo.toml": true, "mods/a.jar": true}
	for _, p := range got {
		if !want[p] {
			t.Errorf("wrapper dir not stripped: %q", p)
		}
	}
}

func TestExtractKeepsSingleKnownGroup(t *testing.T) {
	z := makeZip(t, map[string]string{"mods/a.jar": "a", "mods/b.jar": "b"})
	for _, p := range extract(t, z, Options{}) {
		if p != "mods/a.jar" && p != "mods/b.jar" {
			t.Errorf("mods/ mistaken for a wrapper dir: %q", p)
		}
	}
}

func TestExtractIncludeAndOptional(t *testing.T) {
	z := makeZip(t, map[string]string{
		"mods/a.jar":  "a",
		"saves/w.dat": "w",
	})
	files, err := Extract(z, blobs(t), Options{Include: []string{"mods", "saves"}, Optional: []string{"saves"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Group == "saves" && !f.Optional {
			t.Error("saves must be optional")
		}
		if f.Group == "mods" && f.Optional {
			t.Error("mods must be required")
		}
	}

	only, err := Extract(z, blobs(t), Options{Include: []string{"mods"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0].Path != "mods/a.jar" {
		t.Fatalf("include=mods returned %v", only)
	}
}

func TestFingerprintChangesWithContent(t *testing.T) {
	b := blobs(t)
	first, err := Extract(makeZip(t, map[string]string{"mods/a.jar": "v1"}), b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	same, err := Extract(makeZip(t, map[string]string{"mods/a.jar": "v1"}), b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	other, err := Extract(makeZip(t, map[string]string{"mods/a.jar": "v2"}), b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(first) != Fingerprint(same) {
		t.Error("same file set must give the same fingerprint")
	}
	if Fingerprint(first) == Fingerprint(other) {
		t.Error("changed file must change the fingerprint")
	}
}

func TestExtractEmptyArchive(t *testing.T) {
	if _, err := Extract(makeZip(t, map[string]string{}), blobs(t), Options{}); err == nil {
		t.Error("empty archive must be an error")
	}
}
