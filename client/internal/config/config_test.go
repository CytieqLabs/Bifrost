package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingConfigDefaultsToLocal(t *testing.T) {
	config, err := (&Store{Path: filepath.Join(t.TempDir(), "config.json")}).Load()
	if err != nil || config.Current != "local" || config.Profiles["local"].Mode != ModeLocal {
		t.Fatalf("unexpected default: %#v, %v", config, err)
	}
}

func TestSaveDoesNotStoreTokenValue(t *testing.T) {
	t.Setenv("BIFROST_TEST_TOKEN", "super-secret")
	path := filepath.Join(t.TempDir(), "bifrost", "config.json")
	store := &Store{Path: path}
	config := &Config{Version: Version, Current: "cloud", Profiles: map[string]Profile{
		"cloud": {Mode: ModeRemote, Endpoint: "https://bifrost.example", TokenEnv: "BIFROST_TEST_TOKEN"},
	}}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions are %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret") {
		t.Fatal("resolved token value was persisted")
	}
	loaded, err := store.Load()
	if err != nil || loaded.Profiles["cloud"].TokenEnv != "BIFROST_TEST_TOKEN" {
		t.Fatalf("profile did not round trip: %#v, %v", loaded, err)
	}
	if token, err := ResolveToken(loaded.Profiles["cloud"]); err != nil || token != "super-secret" {
		t.Fatalf("token source did not resolve: %q, %v", token, err)
	}
}

func TestRemoteProfileRejectsEmbeddedCredentials(t *testing.T) {
	err := ValidateProfile(Profile{Mode: ModeRemote, Endpoint: "https://user:secret@example.com"})
	if err == nil {
		t.Fatal("embedded credentials were accepted")
	}
}
