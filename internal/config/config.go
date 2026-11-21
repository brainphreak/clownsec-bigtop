// Package config holds Big Top's settings and where its data lives.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	DefaultPort   = 32500 // HTTP: the 3DS API, the web page, the video files
	DiscoveryPort = 32501 // UDP: "is there a Big Top here?"
)

type Config struct {
	mu   sync.Mutex
	path string

	Name             string `json:"name"` // shown on the 3DS server list
	ID               string `json:"id"`   // stable: the 3DS keys its settings by it
	Port             int    `json:"port"`
	AllowRemoteAdmin bool   `json:"allowRemoteAdmin"`     // the setup page from other computers (off: this PC only)
	FastMoflex       bool   `json:"fastMoflex,omitempty"` // Old 3DS streams: the faster motion search (slower computers)
}

// DataDir is %APPDATA%\Clownsec Big Top on Windows, ~/Library/Application Support/Clownsec Big Top
// on macOS, ~/.config/clownsec-bigtop on Linux -- or the folder given with -data.
func DataDir(override string) (string, error) {
	dir := override
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		name := "Clownsec Big Top"
		if runtime.GOOS == "linux" {
			name = "clownsec-bigtop" // Linux convention: no spaces in ~/.config
		}
		dir = filepath.Join(base, name)
	}
	return dir, os.MkdirAll(dir, 0o755)
}

func Load(dir string) (*Config, error) {
	c := &Config{path: filepath.Join(dir, "config.json"), Port: DefaultPort}
	if b, err := os.ReadFile(c.path); err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	changed := false
	if c.ID == "" {
		b := make([]byte, 8)
		rand.Read(b)
		c.ID, changed = hex.EncodeToString(b), true
	}
	if c.Name == "" {
		h, _ := os.Hostname()
		h = strings.TrimSuffix(h, ".local") // macOS hostnames carry the mDNS suffix
		if h == "" {
			h = "Big Top"
		}
		c.Name, changed = h, true
	}
	if c.Port == 0 {
		c.Port, changed = DefaultPort, true
	}
	if changed {
		return c, c.Save()
	}
	return c, nil
}

func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Update changes settings under the lock and saves.
func (c *Config) Update(f func(*Config)) error {
	c.mu.Lock()
	f(c)
	c.mu.Unlock()
	return c.Save()
}

// Snapshot is a copy safe to read without the lock.
func (c *Config) Snapshot() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Config{Name: c.Name, ID: c.ID, Port: c.Port, AllowRemoteAdmin: c.AllowRemoteAdmin}
}
