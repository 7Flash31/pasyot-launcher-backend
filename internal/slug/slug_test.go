package slug

import "testing"

func TestMake(t *testing.T) {
	cases := map[string]string{
		"Пасёт SMP":      "pasyot-smp",
		"Пасёт  Хардкор": "pasyot-hardkor",
		"Modpack v1.2":   "modpack-v1-2",
		"  Ёлки  ":       "yolki",
		"---":            "",
		"日本":             "",
	}
	for in, want := range cases {
		if got := Make(in); got != want {
			t.Errorf("Make(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValid(t *testing.T) {
	ok := []string{"pasyot-smp", "pack_1", "a"}
	bad := []string{"", "-pack", "pack-", "Пасёт", "pack/../etc", "PACK", "a b"}
	for _, s := range ok {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}
