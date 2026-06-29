// Package clone is kage's engine: it ties the Chrome pool, the JavaScript
// stripper, the asset localiser, and the URL↔path mapper into one resumable,
// polite crawl that turns a live site into a browsable offline folder.
package clone

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/kage/urlx"
)

// Cookie is a single name=value cookie kage sends with every request it makes
// during a clone — the Chrome page navigations, the asset downloads, and the
// robots.txt and sitemap fetches — so a site behind a login or a cookie wall
// can still be mirrored.
type Cookie struct {
	Name  string
	Value string
}

// DefaultOutDir is where mirrors land unless --out overrides it: a per-user data
// directory ($HOME/data/kage) so clones from anywhere collect in one place,
// falling back to a local kage-out when the home directory cannot be resolved.
func DefaultOutDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "kage-out"
	}
	return filepath.Join(home, "data", "kage")
}

// Config is the full set of knobs for a clone run. DefaultConfig fills the
// baseline; the CLI overlays flags on top.
type Config struct {
	OutDir   string // output root; the mirror lands in <OutDir>/<host>/
	Reserved string // reserved dir name for assets and state (default "_kage")

	Workers       int // page render workers
	AssetWorkers  int // HTTP asset download workers
	BrowserPages  int // Chrome page-pool size
	MaxPages      int // stop after N pages (0 = unlimited)
	MaxDepth      int // BFS/DFS depth cap (0 = unlimited)
	Traversal     string
	MaxAssetBytes int64

	// AssetSameDomain, when set, localizes only assets whose host shares the
	// seed's registrable domain (apple.com covers developer.apple.com and
	// www.apple.com but not cdn-apple.com or an unrelated third party). An
	// off-domain asset is left pointing at its live URL instead of downloaded.
	AssetSameDomain bool
	// SkipAssetExts lists asset extensions (".mp4", ".pdf", ".dmg", …) that are
	// left on the live web rather than downloaded, so bulk media, installers, and
	// archives do not bloat the mirror. The reference keeps its remote URL.
	SkipAssetExts map[string]bool

	Timeout       time.Duration // per HTTP request
	Settle        time.Duration // network-idle quiet period
	RenderTimeout time.Duration // hard cap per page render
	Scroll        bool

	UserAgent string
	// Cookies are sent with every request kage makes during the run (the Chrome
	// page renders, the asset downloads, and the robots.txt and sitemap fetches),
	// scoped to the seed host and its subdomains, so a login- or region-gated site
	// can be cloned. They are empty by default.
	Cookies           []Cookie
	IncludeSubdomains bool
	ScopePrefix       string
	ExcludePaths      []string

	RespectRobots  bool
	CrawlDelay     time.Duration // override robots.txt Crawl-delay when > 0
	FollowSitemap  bool
	Headless       bool
	KeepNoscript   bool
	MobileReadable bool
	ChromeBin      string
	ControlURL     string

	// Resume loads the prior run's visited set and skips pages already written,
	// so an interrupted or repeated clone picks up where it left off instead of
	// refetching. Refresh forces every page to be re-rendered in place (the
	// mirror is kept, files are overwritten) to pull in changed content. Force
	// deletes the mirror first for a clean-slate clone. Persist writes the
	// visited set back to state.json when the run ends.
	Resume  bool
	Refresh bool
	Force   bool
	Persist bool
}

// DefaultUserAgent is a current desktop Chrome UA, used by the asset fetcher and
// the robots fetch so a site treats kage like the browser it drives.
const DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// DefaultSkipAssetExts returns the asset extensions kage leaves on the live web
// by default: bulk media, installers, and archives that rarely matter for
// reading a site offline but dominate its download size (a docs site's WWDC
// videos, .dmg/.pkg installers, and PDF manuals can be most of the bytes).
// Page-rendering assets (images, fonts, CSS) are deliberately absent, so the
// offline pages still look right.
func DefaultSkipAssetExts() map[string]bool {
	exts := []string{
		// Video and audio.
		".mp4", ".m4v", ".mov", ".avi", ".mkv", ".webm", ".flv", ".wmv",
		".m3u8", ".ts", ".mp3", ".wav", ".flac", ".aac", ".ogg", ".oga",
		// Installers and disk images.
		".dmg", ".pkg", ".exe", ".msi", ".deb", ".rpm", ".appimage", ".iso",
		// Archives.
		".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar",
		// Documents that download rather than render.
		".pdf",
	}
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		m[e] = true
	}
	return m
}

// DefaultConfig returns the baseline configuration.
func DefaultConfig() Config {
	return Config{
		OutDir:          DefaultOutDir(),
		Reserved:        urlx.DefaultReserved,
		Workers:         4,
		AssetWorkers:    8,
		BrowserPages:    4,
		MaxAssetBytes:   25 << 20,
		AssetSameDomain: true,
		SkipAssetExts:   DefaultSkipAssetExts(),
		Traversal:       "bfs",
		Timeout:         30 * time.Second,
		Settle:          1500 * time.Millisecond,
		RenderTimeout:   30 * time.Second,
		UserAgent:       DefaultUserAgent,
		RespectRobots:   true,
		FollowSitemap:   true,
		Headless:        true,
		Resume:          true,
		Persist:         true,
	}
}

// CookieHeader serialises the configured cookies into a value for the Cookie
// request header ("a=1; b=2"), or "" when none are set. Empty-named entries are
// skipped so a stray pair never produces a malformed header.
func (c Config) CookieHeader() string {
	parts := make([]string, 0, len(c.Cookies))
	for _, ck := range c.Cookies {
		if ck.Name == "" {
			continue
		}
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	return strings.Join(parts, "; ")
}

// HostDir returns the mirror root for a seed host: <OutDir>/<host>.
func (c Config) HostDir(host string) string {
	return filepath.Join(c.OutDir, host)
}

// scope builds the urlx scope config from the run config.
func (c Config) scope() urlx.ScopeConfig {
	return urlx.ScopeConfig{
		IncludeSubdomains: c.IncludeSubdomains,
		ScopePrefix:       c.ScopePrefix,
		ExcludePaths:      c.ExcludePaths,
	}
}
