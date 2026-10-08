package config

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadConfig(t *testing.T) {
	t.Setenv("RAZVHOST_TEST_ALIAS", "alias.test")
	input := `
# comments and blank lines are ignored
example.test {{env "RAZVHOST_TEST_ALIAS"}} -> http://localhost:8080 https://localhost:8443/base?q=1
invalid line
bad.test -> http://%zz
files.test/public -> file:///srv/public
`
	entries, err := ReadConfig(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"example.test -> http://localhost:8080",
		"example.test -> https://localhost:8443/base?q=1",
		"alias.test -> http://localhost:8080",
		"alias.test -> https://localhost:8443/base?q=1",
		"files.test/public -> file:///srv/public",
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Hostname+" -> "+entry.Target.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

func TestReadConfigErrors(t *testing.T) {
	for _, input := range []string{
		`{{`,
		`{{fail "template failed"}}`,
		strings.Repeat("a", 70*1024), // exceeds bufio.Scanner's line limit
	} {
		t.Run(input[:min(len(input), 30)], func(t *testing.T) {
			if _, err := ReadConfig(strings.NewReader(input)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
	wantErr := errors.New("read failed")
	if _, err := ReadConfig(configErrorReader{wantErr}); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

type configErrorReader struct{ err error }

func (r configErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadConfigFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "config")
	if _, err := ReadConfigFile(filename); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file error = %v", err)
	}
	if err := os.WriteFile(filename, []byte("example.test -> http://localhost:8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadConfigFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Hostname != "example.test" || entries[0].Target.String() != "http://localhost:8080" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestGetConfigChange(t *testing.T) {
	entry := func(host, target string) ConfigEntry {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		return ConfigEntry{Hostname: host, Target: *u}
	}
	a := entry("a.test", "http://localhost:8080")
	b := entry("b.test", "http://localhost:8081")
	changed := entry("a.test", "http://localhost:8082")
	for _, tt := range []struct {
		name       string
		prev, next []ConfigEntry
		up, down   configEntries
	}{
		{name: "initial", next: []ConfigEntry{a, b}, up: configEntries{a, b}},
		{name: "reordered", prev: []ConfigEntry{a, b}, next: []ConfigEntry{b, a}},
		{name: "changed target", prev: []ConfigEntry{a, b}, next: []ConfigEntry{changed, b}, up: configEntries{changed}, down: configEntries{a}},
		{name: "removed all", prev: []ConfigEntry{a, b}, down: configEntries{a, b}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{prevEntries: tt.prev}
			up, down := cfg.getConfigChange(tt.next)
			if !reflect.DeepEqual(up, tt.up) || !reflect.DeepEqual(down, tt.down) {
				t.Fatalf("changes = (%v, %v), want (%v, %v)", up, down, tt.up, tt.down)
			}
		})
	}
}

func TestConfigEvents(t *testing.T) {
	target, err := url.Parse("sftp://user:secret@example.test/dir")
	if err != nil {
		t.Fatal(err)
	}
	entry := ConfigEntry{Hostname: "files.test", Target: *target}
	for _, up := range []bool{true, false} {
		events := configEntries{entry}.toEvents(up)
		if len(events) != 1 || !reflect.DeepEqual(events[0].ConfigEntry, entry) || events[0].Up != up {
			t.Fatalf("unexpected events: %+v", events)
		}
		state := "[DOWN]"
		if up {
			state = "[UP]"
		}
		want := "files.test -> sftp://user:xxxxx@example.test/dir " + state
		if got := events[0].String(); got != want {
			t.Fatalf("event string = %q, want %q", got, want)
		}
	}
}
