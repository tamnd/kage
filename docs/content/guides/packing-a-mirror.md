---
title: "Packing a mirror"
description: "Turn a cloned folder into one ZIM file or a self-contained offline viewer with kage pack."
weight: 30
---

A clone is a folder of files, which is easy to browse but awkward to move around: copying thousands of small files is slow, and handing someone a directory is less tidy than handing them one file. `kage pack` collapses a mirror into a single distributable artifact, and you choose the shape: an open ZIM archive, or a self-contained executable that serves the site offline when run.

The two examples below assume you have already cloned a site, for instance Paul Graham's essays:

```bash
kage clone paulgraham.com
```

## A single ZIM file

ZIM is the open, single-file offline-archive format Kiwix uses. `kage pack` writes one from a cloned host directory:

```bash
kage pack paulgraham.com
```

```
packed paulgraham.com.zim
  size 4.2 MiB
  open kage open paulgraham.com.zim
```

The whole mirror, pages and assets, lives in that one file. Text is zstd compressed; already-compressed media (images, fonts, video) is stored as-is. Packing the same mirror twice produces a byte-identical file, so a ZIM is safe to checksum, diff, and cache.

If you cloned with the default output directory, you can pass a bare host name and kage finds the mirror for you. That is why `kage pack paulgraham.com` works straight after `kage clone paulgraham.com`; pass a full path if your mirror lives somewhere else.

### What ZIM is, and using it with Kiwix

ZIM is built for exactly this job: a whole website (or a whole Wikipedia) squeezed into one compressed, indexed, read-only file. It is the format behind [Kiwix](https://kiwix.org), the offline-content project people use to carry Wikipedia, Stack Overflow, and Project Gutenberg onto boats, into classrooms with no internet, and onto a phone for a long flight. Because the format is a documented standard rather than a kage invention, a `paulgraham.com.zim` you make today still opens in any ZIM reader years from now.

So you are not locked into kage. `kage open` is the read side and the quickest way back in, but the same file works across the wider Kiwix ecosystem:

```bash
kage open paulgraham.com.zim            # read it back with kage
kiwix-serve paulgraham.com.zim          # or serve it with Kiwix at http://localhost
```

You can also double-click the file in the [Kiwix desktop app](https://kiwix.org/en/applications/), or load it on Kiwix for Android or iOS to read your mirror on your phone. One caveat: kage writes a structurally valid archive with the standard metadata, but it does not write the full-text search index that Kiwix's own packs ship with, so browsing works everywhere while in-reader search is limited.

## A self-contained binary

`--format binary` appends the ZIM to a copy of kage, producing one executable that *is* the site. Run it and it serves the mirror on a free local port and opens your browser; it ignores its arguments, because the binary is the site, not the kage CLI.

```bash
kage pack paulgraham.com --format binary -o paulgraham
```

```
packed paulgraham
  size 21.9 MiB
  run ./paulgraham to view the site offline
```

```bash
./paulgraham
```

```
serving offline site at http://127.0.0.1:52431  (Ctrl-C to stop)
```

The binary carries a full kage, so it is tens of megabytes regardless of site size; the trade is that the recipient needs nothing installed, not even kage, not even a ZIM reader.

### A native window instead of a browser

By default the viewer opens the system browser, which means a tab with an address bar and your other tabs alongside. Build kage with the `webview` tag and it opens the site in its own native window instead, backed by the operating system's WebView (WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux), so a packed binary feels like a standalone app:

```bash
CGO_ENABLED=1 go build -tags webview -o kage ./cmd/kage
kage pack paulgraham.com --format binary --base kage -o paulgraham
./paulgraham   # opens a window, no browser
```

![paulgraham.com served offline in a native kage window](/webview.png)

The window title comes from the archive's title. This build needs cgo and links the platform WebView, so it is opt-in and kept out of the default `CGO_ENABLED=0` release; the prebuilt binaries open the browser. `kage open` honours the same tag: built with `-tags webview` it shows the ZIM in a native window too.

### Build a viewer for another platform

The appended archive is platform-independent; only the base executable carries the architecture. Point `--base` at a kage binary built for another OS (download one from a kage release) to produce a viewer for that platform from your own machine:

```bash
# From macOS, build a Windows viewer
kage pack paulgraham.com --format binary --base kage-windows-amd64.exe
# -> paulgraham.exe
```

### macOS note

A binary you built or downloaded may be quarantined by Gatekeeper on first run. kage prints the exact command to clear it:

```bash
xattr -d com.apple.quarantine ./paulgraham
```

## Metadata and options

```bash
kage pack paulgraham.com \
  --title "Paul Graham, offline" \
  --description "A snapshot taken for archival" \
  --language eng \
  --date 2026-06-14
```

`--title` defaults to the main page's `<title>`, then the host name. `--date` defaults to today; pass a fixed value for a fully reproducible file. `--no-compress` stores every cluster raw, which packs fastest and lets a reader without zstd open the result. `-o/--out` overrides the output path for either format.
