package browser

import "runtime"

func launcherLeakless() bool {
	return runtime.GOOS != "windows"
}
