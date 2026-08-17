package domain

import "testing"

func TestValidLoader(t *testing.T) {
	ok := []string{"", "vanilla", "forge", "neoforge", "fabric", "quilt"}
	bad := []string{"Forge", "forge ", "liteloader", "fabric-loader", "0"}
	for _, s := range ok {
		if !ValidLoader(s) {
			t.Errorf("ValidLoader(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidLoader(s) {
			t.Errorf("ValidLoader(%q) = true, want false", s)
		}
	}
}
