// Package enginedl downloads + caches a game module's compiled engine shared
// library from the server, so local testing drives the EXACT engine the server runs
// with no manual build. It is a Go port of the Python tool's simcode/_engine_dl.py.
//
// The engine is per game module (Robot City has its own). Rather than ask users to
// build the c-shared themselves, we fetch it from the module's distribution endpoint:
//
//	GET {server}/api/engine/version?module=<m>           → {"module","version","platforms"}
//	GET {server}/api/engine/lib?module=<m>&os=..&arch=.. → the raw .so bytes
//
// The library is cached at ~/.cache/simcode/engine-<module>-<version>-<platform>.so
// and re-used when the cached module+version matches. The server base URL is
// $SIMCODE_SERVER (default https://robocity.lyabah.com). $SIMCODE_ENGINE_SO overrides
// everything with an explicit local build (used by the smoke test + engine devs).
package enginedl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultServer is the public server used when $SIMCODE_SERVER is unset.
const DefaultServer = "https://robocity.lyabah.com"

// ServerBase returns the server base URL ($SIMCODE_SERVER or the default), no slash.
func ServerBase() string {
	s := os.Getenv("SIMCODE_SERVER")
	if s == "" {
		s = DefaultServer
	}
	return strings.TrimRight(s, "/")
}

// LocalPlatform detects this machine's <os>-<arch> token used by the server's
// filenames / query params (e.g. "linux-amd64"). Go's runtime.GOOS/GOARCH already use
// the same vocabulary (linux/darwin/windows, amd64/arm64) as the server.
func LocalPlatform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// CacheDir returns the per-user cache dir (XDG_CACHE_HOME aware), created on demand.
func CacheDir() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	d := filepath.Join(base, "simcode")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func httpGet(url string) (*http.Response, error) {
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Get(url) //nolint:noctx // trusted URL, short-lived CLI
	if err != nil {
		return nil, fmt.Errorf("GET %s failed (%v); is the server reachable? "+
			"Set SIMCODE_SERVER or SIMCODE_ENGINE_SO to override", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s → HTTP %d %s: %s", url, resp.StatusCode, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

// versionDoc is the /api/engine/version response.
type versionDoc struct {
	Module    string   `json:"module"`
	Version   string   `json:"version"`
	Platforms []string `json:"platforms"`
}

// FetchVersion returns (version, platforms) from /api/engine/version?module=<module>.
func FetchVersion(module, server string) (string, []string, error) {
	server = strings.TrimRight(server, "/")
	url := fmt.Sprintf("%s/api/engine/version?module=%s", server, module)
	resp, err := httpGet(url)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	var doc versionDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", nil, fmt.Errorf("%s returned an unreadable version doc: %w", url, err)
	}
	if doc.Version == "" {
		return "", nil, fmt.Errorf("%s returned no version for module %q", url, module)
	}
	return doc.Version, doc.Platforms, nil
}

// EnsureEngine resolves a usable engine .so path for module on this machine,
// downloading + caching it from the server if needed. Returns the local path.
//
//   - $SIMCODE_ENGINE_SO (if set) wins — an explicit dev/local build (any module).
//   - else GET the module's server version, and if the matching cached file exists,
//     return it; otherwise download + cache it.
func EnsureEngine(module string) (string, error) {
	if override := os.Getenv("SIMCODE_ENGINE_SO"); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("SIMCODE_ENGINE_SO=%q does not exist", override)
		}
		return override, nil
	}

	server := ServerBase()
	plat := LocalPlatform()
	version, platforms, err := FetchVersion(module, server)
	if err != nil {
		return "", err
	}
	if len(platforms) > 0 && !contains(platforms, plat) {
		return "", fmt.Errorf("the server has no %q engine library for this platform (%s); available: %s. "+
			"Build one locally and point SIMCODE_ENGINE_SO at it, or run on a linux-amd64 (glibc) host",
			module, plat, strings.Join(platforms, ", "))
	}

	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	cached := filepath.Join(dir, fmt.Sprintf("engine-%s-%s-%s.so", module, version, plat))
	if fi, err := os.Stat(cached); err == nil && fi.Size() > 0 {
		return cached, nil
	}

	osName, arch, _ := strings.Cut(plat, "-")
	url := fmt.Sprintf("%s/api/engine/lib?module=%s&os=%s&arch=%s", server, module, osName, arch)
	resp, err := httpGet(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("%s returned an empty library", url)
	}

	// Write atomically (temp + rename) so a concurrent run never sees a half file.
	tmp := fmt.Sprintf("%s.%d.tmp", cached, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, cached); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return cached, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
