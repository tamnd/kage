---
title: "Packing a mirror"
description: "Turn a cloned folder into one ZIM file or a self-contained offline viewer with kage pack."
weight: 30
---

A clone is a folder of files, which is easy to browse but awkward to move around:
copying thousands of small files is slow, and handing someone a directory is less
tidy than handing them one file. `kage pack` collapses a mirror into a single
distributable artifact, either an open ZIM archive or a self-contained executable
that serves the site offline when run.

## Pack to a ZIM file

ZIM is the open, single-file offline-archive format Kiwix uses. `kage pack`
writes one from a cloned host directory:

```bash
kage pack kage-out/example.com
```

```
packed example.com.zim
  size 4.2 MiB
  open kage open example.com.zim
```

The whole mirror, pages and assets, lives in that one file. Text is zstd
compressed; already-compressed media (images, fonts, video) is stored as-is.
Packing the same mirror twice produces a byte-identical file, so a ZIM is safe to
checksum, diff, and cache.

If you cloned with the default output directory, you can pass a bare host name and
kage finds the mirror for you:

```bash
kage clone example.com
kage pack example.com
```

### Read it back

`kage open` is the read side: it serves a ZIM over a local HTTP server and opens
your browser, the same way `kage serve` does for a folder.

```bash
kage open example.com.zim
```

```
kage open example.com.zim
  open http://127.0.0.1:8800
  press Ctrl-C to stop
```

Any other ZIM reader (Kiwix among them) can also open the file. kage writes a
structurally valid archive with the standard metadata; full-text search indexes
are not written, so browsing works everywhere but in-reader search is limited.

## Pack to a self-contained binary

`--format binary` appends the ZIM to a copy of kage, producing one executable
that *is* the site. Run it and it serves the mirror on a free local port and
opens your browser; it ignores its arguments, because the binary is the site, not
the kage CLI.

```bash
kage pack kage-out/example.com --format binary
```

```
packed example
  size 21.9 MiB
  run ./example to view the site offline
```

```bash
./example
```

```
serving offline site at http://127.0.0.1:52431  (Ctrl-C to stop)
```

The binary carries a full kage, so it is tens of megabytes regardless of site
size; the trade is that the recipient needs nothing installed, not even kage.

### A native window instead of a browser

By default the viewer opens the system browser, which means a tab with an address
bar and your other tabs alongside. Build kage with the `webview` tag and it opens
the site in its own native window instead, backed by the operating system's
WebView (WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux), so a
packed binary feels like a standalone app:

```bash
CGO_ENABLED=1 go build -tags webview -o kage ./cmd/kage
kage pack kage-out/example.com --format binary --base kage
./example   # opens a window, no browser
```

The window title comes from the archive's title. This build needs cgo and links
the platform WebView, so it is opt-in and kept out of the default
`CGO_ENABLED=0` release; the prebuilt binaries open the browser. `kage open` honours
the same tag: built with `-tags webview` it shows the ZIM in a native window too.

### Build a viewer for another platform

The appended archive is platform-independent; only the base executable carries
the architecture. Point `--base` at a kage binary built for another OS (download
one from a kage release) to produce a viewer for that platform from your own
machine:

```bash
# From macOS, build a Windows viewer
kage pack kage-out/example.com --format binary --base kage-windows-amd64.exe
# -> example.exe
```

### macOS note

A binary you built or downloaded may be quarantined by Gatekeeper on first run.
kage prints the exact command to clear it:

```bash
xattr -d com.apple.quarantine ./example
```

## Metadata and options

```bash
kage pack kage-out/example.com \
  --title "Example, offline" \
  --description "A snapshot taken for archival" \
  --language eng \
  --date 2026-06-14
```

`--title` defaults to the main page's `<title>`, then the host name. `--date`
defaults to today; pass a fixed value for a fully reproducible file. `--no-compress`
stores every cluster raw, which packs fastest and lets a reader without zstd open
the result. `-o/--out` overrides the output path for either format.
