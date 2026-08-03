//go:build !windows

package browser

import (
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
)

type rodChromeLauncher struct {
	launcher *launcher.Launcher
}

func newChromeLauncher() chromeLauncher {
	return &rodChromeLauncher{launcher: launcher.New()}
}

func (l *rodChromeLauncher) Leakless(enable bool) chromeLauncher {
	l.launcher.Leakless(enable)
	return l
}

func (l *rodChromeLauncher) Headless(enable bool) chromeLauncher {
	l.launcher.Headless(enable)
	return l
}

func (l *rodChromeLauncher) Set(name string, values ...string) chromeLauncher {
	l.launcher.Set(flags.Flag(name), values...)
	return l
}

func (l *rodChromeLauncher) Bin(path string) chromeLauncher {
	l.launcher.Bin(path)
	return l
}

func (l *rodChromeLauncher) Launch() (string, error) {
	return l.launcher.Launch()
}

func launcherLookPath() (string, bool) {
	return launcher.LookPath()
}
