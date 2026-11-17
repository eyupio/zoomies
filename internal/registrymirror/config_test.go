package registrymirror

import "testing"

// A mirror with no folder has nowhere to keep anything, and one with no bound
// would keep everything until the disk was full, so neither is a mirror that
// starts. There is deliberately no "unbounded".
func TestNewRefusesAConfigurationItCannotRunWith(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no folder", Config{MaxBytes: 1 << 20}},
		{"no bound", Config{Dir: t.TempDir()}},
		{"a negative bound", Config{Dir: t.TempDir(), MaxBytes: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if m, err := New(c.cfg); err == nil {
				t.Fatalf("New accepted %+v and returned %v", c.cfg, m)
			}
		})
	}
}

func TestNewCreatesTheFolderItKeepsThingsIn(t *testing.T) {
	dir := t.TempDir() + "/not/there/yet"

	if _, err := New(Config{Dir: dir, MaxBytes: 1 << 20}); err != nil {
		t.Fatalf("New: %v", err)
	}

	if !dirExists(dir) {
		t.Errorf("%s was not created", dir)
	}
}
