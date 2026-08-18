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

func TestValidMinecraft(t *testing.T) {
	ok := []string{"", "1.20.1", "1.21", "24w14a", "1.20.1-rc1", "1.7.10"}
	bad := []string{"1.20.1 ", "версия", "latest release", "v1.20", "1.20.1.1.1.1.1.1.1"}
	for _, s := range ok {
		if !ValidMinecraft(s) {
			t.Errorf("ValidMinecraft(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidMinecraft(s) {
			t.Errorf("ValidMinecraft(%q) = true, want false", s)
		}
	}
}

func TestName(t *testing.T) {
	normalized := map[string]string{
		"Pasyot SMP":   "pasyot-smp",
		"  hardcore  ": "hardcore",
		"Pasyot   SMP": "pasyot-smp",
		"MyPack_2":     "mypack_2",
	}
	for in, want := range normalized {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
	ok := []string{"pasyot-smp", "hardcore", "pack_2", "a"}
	bad := []string{"", "-pack", "Пасёт", "pasyot smp", "PASYOT", "pack/../etc", "-", "паcyot"}
	for _, s := range ok {
		if !ValidName(s) {
			t.Errorf("ValidName(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if ValidName(s) {
			t.Errorf("ValidName(%q) = true, want false", s)
		}
	}
	for _, s := range []string{"Пасёт SMP", "Хардкор"} {
		if ValidName(NormalizeName(s)) {
			t.Errorf("cyrillic %q must not pass after normalization", s)
		}
	}
}
