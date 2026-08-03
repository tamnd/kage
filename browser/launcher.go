package browser

// chromeLauncher is the small part of Rod's launcher kage needs. The
// platform-specific implementations keep Rod's launcher (and therefore its
// embedded leakless watchdog) out of Windows binaries while retaining the
// upstream launcher on platforms where leakless is enabled.
type chromeLauncher interface {
	Leakless(bool) chromeLauncher
	Headless(bool) chromeLauncher
	Set(string, ...string) chromeLauncher
	Bin(string) chromeLauncher
	Launch() (string, error)
}
