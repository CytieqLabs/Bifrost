package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const Version = 1

type Mode string

const (
	ModeLocal  Mode = "local"
	ModeRemote Mode = "remote"
)

type Profile struct {
	Mode      Mode   `json:"mode"`
	Endpoint  string `json:"endpoint,omitempty"`
	TokenEnv  string `json:"token_env,omitempty"`
	TokenFile string `json:"token_file,omitempty"`
}

type Config struct {
	Version  int                `json:"version"`
	Current  string             `json:"current"`
	Profiles map[string]Profile `json:"profiles"`
}

type Store struct{ Path string }

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func DefaultStore() (*Store, error) {
	if path := os.Getenv("BIFROST_CONFIG"); path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		return &Store{Path: absolute}, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{Path: filepath.Join(dir, "bifrost", "config.json")}, nil
}

func Default() *Config {
	return &Config{Version: Version, Current: "local", Profiles: map[string]Profile{"local": {Mode: ModeLocal}}}
}

func (s *Store) Load() (*Config, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return nil, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("invalid Bifrost config: %w", err)
	}
	if config.Version != Version {
		return nil, fmt.Errorf("unsupported Bifrost config version %d", config.Version)
	}
	if config.Profiles == nil {
		config.Profiles = map[string]Profile{}
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (s *Store) Save(config *Config) error {
	config.Version = Version
	if err := config.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.Path), "config-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.Path)
}

func (c *Config) Validate() error {
	if !namePattern.MatchString(c.Current) {
		return errors.New("current profile name is invalid")
	}
	if _, ok := c.Profiles[c.Current]; !ok {
		return fmt.Errorf("current profile %q does not exist", c.Current)
	}
	for name, profile := range c.Profiles {
		if !namePattern.MatchString(name) {
			return fmt.Errorf("profile name %q is invalid", name)
		}
		if err := ValidateProfile(profile); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
	}
	return nil
}

func ValidateProfile(profile Profile) error {
	switch profile.Mode {
	case ModeLocal:
		if profile.Endpoint != "" || profile.TokenEnv != "" || profile.TokenFile != "" {
			return errors.New("local profiles cannot define endpoint or token settings")
		}
	case ModeRemote:
		parsed, err := url.Parse(profile.Endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return errors.New("remote endpoint must be an absolute HTTP or HTTPS origin")
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return errors.New("remote endpoint must not include credentials, path, query, or fragment")
		}
		if profile.TokenEnv != "" && profile.TokenFile != "" {
			return errors.New("use only one token source")
		}
		if profile.TokenEnv != "" && !envPattern.MatchString(profile.TokenEnv) {
			return errors.New("token environment variable name is invalid")
		}
		if profile.TokenFile != "" && !filepath.IsAbs(profile.TokenFile) {
			return errors.New("token file must be an absolute path")
		}
	default:
		return fmt.Errorf("mode must be %q or %q", ModeLocal, ModeRemote)
	}
	return nil
}

func ResolveToken(profile Profile) (string, error) {
	if profile.TokenEnv != "" {
		token := os.Getenv(profile.TokenEnv)
		if token == "" {
			return "", fmt.Errorf("token environment variable %s is empty", profile.TokenEnv)
		}
		return token, nil
	}
	if profile.TokenFile != "" {
		data, err := os.ReadFile(profile.TokenFile)
		if err != nil {
			return "", fmt.Errorf("read profile token: %w", err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", errors.New("profile token file is empty")
		}
		return token, nil
	}
	return "", nil
}
