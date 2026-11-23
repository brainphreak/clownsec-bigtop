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
	FastMoflex       bool   `json:"fastMoflex,omitempty"` // before EncodeMode: Fast ticked (read as EncodeMode "fast")
	// Old 3DS streams: "auto" (default: Best quality if this computer manages 3D at AutoMinSpeed
	// or faster, else Fast), "best", or "fast" (the faster motion search, for slower computers)
	EncodeMode string  `json:"encodeMode,omitempty"`
	Auto3D     float64 `json:"auto3d,omitempty"`  // Auto's measurement: best-quality 3D, x real time
	AutoVer    string  `json:"autoVer,omitempty"` // the Big Top version that measured it (an update measures again)
	// 2D to 3D (internal/depth3d): "" off, else the depth size the setup picked -- "322", "392",
	// "448" (Distill-Any-Depth input width) or "midas"; Depth3DFps what this computer measured with it
	Depth3D    string  `json:"depth3d,omitempty"`
	Depth3DFps float64 `json:"depth3dFps,omitempty"`
	// the setup's measurement of whole converted streams at that size (x real time): an Old 3DS
	// moflex, a New 3DS H.264; the 3DS offers Play in 3D only where it is >= Depth3DMin
	Depth3DOld float64 `json:"depth3dOld,omitempty"`
	Depth3DNew float64 `json:"depth3dNew,omitempty"`
	// the browser's own depth size (its eyes are bigger, its encode heavier) and its measured speed
	Depth3DWeb    string  `json:"depth3dWeb,omitempty"`
	Depth3DWebSpd float64 `json:"depth3dWebSpd,omitempty"`
	SetupDone     bool    `json:"setupDone,omitempty"` // the first-run setup (speed tests, 2D to 3D) was run
	// TMDB: missing movie / show info from The Movie Database -- on unless TMDBOff, through
	// clownsec.com unless TMDBKey (the user's own v3 API key or v4 read-access token)
	TMDBOff bool   `json:"tmdbOff,omitempty"`
	TMDBKey string `json:"tmdbKey,omitempty"`
}

// Depth3DMin: a converted stream must be made this much faster than it plays to be offered.
const Depth3DMin = 1.2

// Conv3D: whether the 3DS may offer Play in 3D -- for an Old 3DS (moflex) and a New 3DS (H.264).
func (c *Config) Conv3D() (old, new bool) {
	if c.Depth3DTier() < 0 {
		return false, false
	}
	return c.Depth3DOld >= Depth3DMin, c.Depth3DNew >= Depth3DMin
}

// Depth3DWebTier: the browser's depth size (-1 = the 3DS's, for a setup from before it was measured).
func (c *Config) Depth3DWebTier() int {
	if c.Depth3DWeb == "" {
		return c.Depth3DTier()
	}
	return tierOf(c.Depth3DWeb)
}

// Depth3DTier: the depth size as depth3d.Engine takes it (-1 = off, 0 = MiDaS).
func (c *Config) Depth3DTier() int { return tierOf(c.Depth3D) }

func tierOf(v string) int {
	switch v {
	case "322":
		return 322
	case "392":
		return 392
	case "448":
		return 448
	case "midas":
		return 0
	}
	return -1
}

// AutoMinSpeed: Auto picks Best quality when 3D encodes at least this much faster than it plays.
const AutoMinSpeed = 1.3

// Mode: the encoding setting, "auto" when nothing was chosen (or an old "fast" tick).
func (c *Config) Mode() string {
	switch c.EncodeMode {
	case "auto", "best", "fast":
		return c.EncodeMode
	}
	if c.FastMoflex {
		return "fast"
	}
	return "auto"
}

// UseFast: whether Old 3DS streams use Fast encoding now. Auto plays safe (Fast) until measured.
func (c *Config) UseFast() bool {
	switch c.Mode() {
	case "best":
		return false
	case "fast":
		return true
	}
	return c.Auto3D < AutoMinSpeed
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
	return Config{Name: c.Name, ID: c.ID, Port: c.Port, AllowRemoteAdmin: c.AllowRemoteAdmin, FastMoflex: c.FastMoflex,
		EncodeMode: c.EncodeMode, Auto3D: c.Auto3D, AutoVer: c.AutoVer,
		Depth3D: c.Depth3D, Depth3DFps: c.Depth3DFps, Depth3DOld: c.Depth3DOld, Depth3DNew: c.Depth3DNew,
		Depth3DWeb: c.Depth3DWeb, Depth3DWebSpd: c.Depth3DWebSpd,
		SetupDone: c.SetupDone, TMDBOff: c.TMDBOff, TMDBKey: c.TMDBKey}
}
