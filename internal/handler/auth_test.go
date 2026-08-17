package handler

import "testing"

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"/admin/modpacks":          "/admin/modpacks",
		"":                         "/",
		"https://evil.example":     "/",
		"//evil.example":           "/",
		"http://evil.example/path": "/",
		"admin":                    "/",
		"/ok?query=1":              "/ok?query=1",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
