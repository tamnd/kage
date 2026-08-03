# Rod controller

This directory contains the controller layer from
[go-rod/rod v0.116.2](https://github.com/go-rod/rod/tree/v0.116.2), under its
original MIT license.

kage always launches Chrome itself and connects through an explicit DevTools
URL. The upstream controller also imports Rod's launcher as an automatic
fallback. That launcher imports `github.com/ysmood/leakless`, whose Windows
package embeds a prebuilt helper that Windows Defender flags. Even with
leakless disabled at runtime, the import links the helper into `kage.exe`.

`browser.go` therefore differs in two functional places: `Connect` requires the
explicit URL kage already supplies, and the unused monitor-opening shortcut is
disabled. A few lint-only spellings are updated for kage's current Go toolchain.
The controller continues to use Rod's public `lib/*` packages so protocol types
and behavior stay aligned with the pinned module version.
