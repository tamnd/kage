// Package leakless is kage's drop-in replacement for
// github.com/ysmood/leakless, wired in through a replace directive in the root
// go.mod.
//
// The upstream package guards a child process by extracting a small helper
// executable that force-kills the child when the parent dies. It ships that
// helper by base64/gzip-embedding a prebuilt binary for every target
// (bin_amd64_windows.go and friends), so the packed leakless.exe ends up linked
// into any program that imports the package, kage included. Antivirus engines
// flag that embedded Windows helper as malware, so a fresh install of kage got
// quarantined before it ever ran (issue #68).
//
// kage already launches Chrome with leakless disabled (see
// browser/leakless.go), so the guard is never used. This stub keeps the exact
// public surface go-rod's launcher depends on (New, Support, LockPort, and the
// Launcher type's Command/Pid/Err) while carrying no embedded binary, which
// removes the false positive entirely. Support reports no guard is available,
// so go-rod's launcher never takes the leakless path even if a caller asked
// for it.
package leakless

import "os/exec"

// Launcher mirrors the upstream type. The channel is left unbuffered and is
// never written to, matching the "may never receive the pid" contract go-rod
// already tolerates.
type Launcher struct {
	pid chan int
}

// New returns a Launcher. It allocates nothing beyond the pid channel.
func New() *Launcher {
	return &Launcher{pid: make(chan int)}
}

// Command builds the command without a guard wrapper. Because Support returns
// false, go-rod never calls this in practice; if some other caller did, running
// the target directly is the correct no-guard behaviour.
func (l *Launcher) Command(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

// Pid returns the (never-signalled) pid channel.
func (l *Launcher) Pid() chan int { return l.pid }

// Err returns the guard error, always empty here since there is no guard.
func (l *Launcher) Err() string { return "" }

// Support reports whether a guard binary is available. It always returns false
// so callers skip leakless entirely.
func Support() bool { return false }

// LockPort is the cross-process mutex the upstream guard uses to serialise
// extraction. With no guard there is nothing to serialise, so it is a no-op.
func LockPort(port int) func() { return func() {} }
