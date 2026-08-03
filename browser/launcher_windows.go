//go:build windows

package browser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ysmood/fetchup"
)

const windowsChromiumRevision = 1321438

type windowsChromeLauncher struct {
	bin   string
	flags map[string][]string
}

func newChromeLauncher() chromeLauncher {
	return &windowsChromeLauncher{flags: map[string][]string{
		"headless":                                           nil,
		"no-first-run":                                       nil,
		"no-startup-window":                                  nil,
		"disable-background-networking":                      nil,
		"disable-background-timer-throttling":                nil,
		"disable-backgrounding-occluded-windows":             nil,
		"disable-breakpad":                                   nil,
		"disable-client-side-phishing-detection":             nil,
		"disable-default-apps":                               nil,
		"disable-hang-monitor":                               nil,
		"disable-popup-blocking":                             nil,
		"disable-prompt-on-repost":                           nil,
		"disable-renderer-backgrounding":                     nil,
		"disable-sync":                                       nil,
		"disable-site-isolation-trials":                      nil,
		"enable-automation":                                  nil,
		"enable-features":                                    {"NetworkService", "NetworkServiceInProcess"},
		"force-color-profile":                                {"srgb"},
		"metrics-recording-only":                             nil,
		"use-mock-keychain":                                  nil,
		"disable-features":                                   {"site-per-process", "TranslateUI"},
		"disable-component-extensions-with-background-pages": nil,
	}}
}

// Leakless is intentionally a no-op. kage has always disabled Rod's watchdog
// on Windows, and avoiding Rod's launcher import is what keeps the watchdog's
// embedded executable out of kage.exe.
func (l *windowsChromeLauncher) Leakless(bool) chromeLauncher { return l }

func (l *windowsChromeLauncher) Headless(enable bool) chromeLauncher {
	if enable {
		l.flags["headless"] = nil
	} else {
		delete(l.flags, "headless")
	}
	return l
}

func (l *windowsChromeLauncher) Set(name string, values ...string) chromeLauncher {
	l.flags[strings.TrimLeft(name, "-")] = values
	return l
}

func (l *windowsChromeLauncher) Bin(path string) chromeLauncher {
	l.bin = path
	return l
}

func (l *windowsChromeLauncher) Launch() (string, error) {
	bin := l.bin
	if bin == "" {
		var err error
		bin, err = windowsChrome()
		if err != nil {
			return "", err
		}
	}

	userDir, err := os.MkdirTemp("", "kage-chrome-")
	if err != nil {
		return "", fmt.Errorf("create Chrome profile: %w", err)
	}

	port, err := freeLocalPort()
	if err != nil {
		_ = os.RemoveAll(userDir)
		return "", fmt.Errorf("reserve Chrome debug port: %w", err)
	}
	l.flags["user-data-dir"] = []string{userDir}
	l.flags["remote-debugging-address"] = []string{"127.0.0.1"}
	l.flags["remote-debugging-port"] = []string{strconv.Itoa(port)}

	var output bytes.Buffer
	cmd := exec.Command(bin, l.formatArgs()...)
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(userDir)
		return "", fmt.Errorf("start Chrome: %w", err)
	}

	exited := make(chan error, 1)
	go func() {
		exited <- cmd.Wait()
		_ = os.RemoveAll(userDir)
	}()

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if u, ok := debuggerURL(endpoint); ok {
			return u, nil
		}
		select {
		case err := <-exited:
			return "", fmt.Errorf("Chrome exited before DevTools was ready: %w\n%s", err, tail(output.String(), 4096))
		case <-deadline.C:
			_ = cmd.Process.Kill()
			<-exited
			return "", fmt.Errorf("Chrome DevTools did not become ready\n%s", tail(output.String(), 4096))
		case <-ticker.C:
		}
	}
}

func (l *windowsChromeLauncher) formatArgs() []string {
	args := make([]string, 0, len(l.flags))
	for name, values := range l.flags {
		arg := "--" + name
		if values != nil {
			arg += "=" + strings.Join(values, ",")
		}
		args = append(args, arg)
	}
	sort.Strings(args)
	return args
}

func launcherLookPath() (string, bool) {
	for _, name := range []string{"chrome", "msedge", "chromium"} {
		if bin, err := exec.LookPath(name); err == nil {
			return bin, true
		}
	}
	for _, bin := range windowsBrowserCandidates() {
		if _, err := os.Stat(bin); err == nil {
			return bin, true
		}
	}
	if bin := cachedWindowsChromium(); bin != "" {
		return bin, true
	}
	return "", false
}

func windowsBrowserCandidates() []string {
	var candidates []string
	for _, root := range []string{
		os.Getenv("PROGRAMFILES"),
		os.Getenv("PROGRAMFILES(X86)"),
		os.Getenv("LOCALAPPDATA"),
	} {
		if root == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(root, "Chromium", "Application", "chrome.exe"),
			filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
		)
	}
	return candidates
}

func windowsChrome() (string, error) {
	if bin, ok := launcherLookPath(); ok {
		return bin, nil
	}

	root := windowsChromiumRoot()
	dir := filepath.Join(root, fmt.Sprintf("chromium-%d", windowsChromiumRevision))
	urls := []string{
		fmt.Sprintf("https://storage.googleapis.com/chromium-browser-snapshots/Win_x64/%d/chrome-win.zip", windowsChromiumRevision),
		fmt.Sprintf("https://registry.npmmirror.com/-/binary/chromium-browser-snapshots/Win_x64/%d/chrome-win.zip", windowsChromiumRevision),
	}
	download := fetchup.New(dir, urls...)
	download.Logger = log.New(io.Discard, "", 0)
	if err := download.Fetch(); err != nil {
		return "", fmt.Errorf("find or download Chrome: %w", err)
	}
	if err := fetchup.StripFirstDir(dir); err != nil {
		return "", fmt.Errorf("unpack Chrome: %w", err)
	}

	bin := filepath.Join(dir, "chrome.exe")
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("downloaded Chrome executable: %w", err)
	}
	return bin, nil
}

func cachedWindowsChromium() string {
	matches, _ := filepath.Glob(filepath.Join(windowsChromiumRoot(), "chromium-*", "chrome.exe"))
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func windowsChromiumRoot() string {
	root := os.Getenv("APPDATA")
	if root == "" {
		root, _ = os.UserCacheDir()
	}
	return filepath.Join(root, "rod", "browser")
}

func freeLocalPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	return port, listener.Close()
}

func debuggerURL(endpoint string) (string, bool) {
	client := http.Client{Timeout: 250 * time.Millisecond}
	response, err := client.Get(endpoint)
	if err != nil {
		return "", false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", false
	}
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(response.Body).Decode(&version) != nil || version.WebSocketDebuggerURL == "" {
		return "", false
	}
	return version.WebSocketDebuggerURL, true
}

func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
