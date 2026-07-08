---
title: "Release notes"
description: "What changed in each kage release."
weight: 40
---

The authoritative, commit-level history lives in [`CHANGELOG.md`](https://github.com/tamnd/kage/blob/main/CHANGELOG.md) and on the [releases page](https://github.com/tamnd/kage/releases). This page summarises each version.

## v0.3.9

A fix for the antivirus warning some Windows users hit when installing kage.

- **The Windows build no longer ships the leakless helper antivirus flags.** kage renders pages with [go-rod](https://github.com/go-rod/rod), whose launcher pulls in [leakless](https://github.com/ysmood/leakless), a small watchdog that force-kills Chrome if kage exits. leakless carries a prebuilt helper binary for every platform and links the Windows one straight into `kage.exe`. Windows Defender recognises that helper as `Trojan:Win32/Kepavll!rfn` and quarantines it, so a fresh `scoop install` failed with a virus warning on `leakless.exe` ([#68](https://github.com/tamnd/kage/issues/68)). kage already launches Chrome with leakless switched off, so the helper never ran anyway. It is now replaced with a stub that carries no embedded binary, which drops about 1.28 MB from the Windows build and clears the warning. Thanks to John Pywtorak for the report. `go install`, unaffected before, stays clean.

## v0.3.4

Two community fixes: a clean stop for `kage serve`, and pages with heavy JavaScript that used to be dropped.

- **`kage serve` stops on Ctrl-C.** The preview server was started with a blocking call that never watched for an interrupt, so stopping it meant killing the process. kage now shuts the server down gracefully on an interrupt or a `SIGTERM`, with a short timeout before forcing the listener closed. Thanks to Xirui Wang ([#35](https://github.com/tamnd/kage/pull/35)) and Kaidi Zhao ([#38](https://github.com/tamnd/kage/pull/38)).
- **Pages with deeply nested JavaScript still clone.** Chrome's DevTools Protocol returns "Object reference chain is too long" while loading a page whose script builds a deeply nested object graph, but the page's HTML has already loaded and the error is only about Chrome's internal object tracking. kage now recognises that error and finishes rendering instead of dropping the page ([#36](https://github.com/tamnd/kage/issues/36)). Thanks to Gautam Kumar ([#39](https://github.com/tamnd/kage/pull/39)).

## v0.3.3

A fix for Chrome saving a file to your Downloads folder mid-crawl.

- **A crawl never writes to your Downloads folder.** A link with no file extension is queued as a page, so the page worker opened it in Chrome. When such a link served a binary, a zip or a CSV, Chrome saved the file to `~/Downloads`, a surprise side effect of running a clone ([#32](https://github.com/tamnd/kage/issues/32)). kage now denies Chrome-initiated downloads outright, since every asset is fetched through kage's own downloader and the browser never needs to write a file. As a second layer, kage detects a navigation whose response is not HTML and reroutes that URL to the asset downloader, where the size and media policy decides whether to localise it or leave it on the live web, instead of saving a broken page.

## v0.3.2

A fix for garbled text on pages that did not carry a charset of their own.

- **Saved pages declare UTF-8.** kage writes every page as UTF-8, but a site that set its charset only in the HTTP `Content-Type` header, with no `<meta charset>` in the markup, lost that signal once the page became a standalone file. A reader serving the bytes without a charset fell back to its locale encoding and turned every curly quote, dash, and non-breaking space into mojibake. kage now inserts a `<meta charset="utf-8">` at the top of `<head>` when the page does not already declare one, so the page renders correctly in any reader.

## v0.3.1

A fix for broken styling when a packed mirror's home page is a nested page.

- **`/` redirects to the main page instead of serving it in place.** A page's saved asset links are mirror-relative (`../_kage/...`), computed for that page's own location. The viewer was answering `/` with the main page's bytes directly, so the browser resolved those links against `/` and 404ed the page's CSS and images. A `developer.apple.com/documentation` mirror opened at `/` came up completely unstyled. kage now redirects `/` to the main page's canonical path, the way the archive's `W/mainPage` redirect does, so relative assets resolve correctly. Kiwix already followed that redirect, so it was never affected.

## v0.3.0

Leaner mirrors, and a way to publish one as a dataset. A clone now keeps the assets that make a site readable offline and leaves the bulk downloads on the live web, and a packed archive converts to a columnar table that drops straight into dataset tooling.

- **Bulk downloads stay remote by default.** Video and audio, installers and disk images (`.dmg`, `.pkg`, `.exe`, `.msi`, ...), archives, and PDFs are left pointing at their live URL rather than downloaded, because they are rarely needed to read a site offline yet routinely make up most of its bytes. On a `developer.apple.com` crawl that class was 18 of 19 GB. Page-rendering assets (images, fonts, CSS) are untouched. `--keep-media` restores the old behaviour, and `--skip-ext .foo` leaves more extensions remote.
- **Assets come only from the site's own domain by default.** Localising is scoped to the seed's registrable domain, so `developer.apple.com` still pulls from `www.apple.com` and `images.apple.com` but not a separate brand domain or an off-topic third party (an embedded tracker, an unrelated CDN). `--all-asset-hosts` downloads from any host as before.
- **The size cap skips instead of truncating.** An asset over `--max-asset-mb` was being saved as exactly the first N MB of itself, a corrupt fragment that would never play or run. kage now checks the response size and leaves an over-cap asset out of the mirror entirely, pointing at its live URL. On the apple crawl this was about a gigabyte of half-downloaded WWDC videos and `.dmg` installers.
- **`kage parquet export` and `import`.** A packed ZIM converts to a flat Parquet table, one row per entry with clear columns (`doc_id`, `url`, `host`, `crawl_date`, `mime`, text, content), the shape a dataset host like [Hugging Face](https://huggingface.co) expects and that DuckDB or pandas reads as is. The column names follow the [open-index/open-markdown](https://huggingface.co/datasets/open-index/open-markdown) dataset, with `doc_id` a deterministic UUID v5 of the page URL, so a kage export sits alongside other web-crawl datasets. The conversion is lossless: a ZIM round-tripped through Parquet reproduces every entry, its metadata, and the main page byte for byte.
- **`kage pack --incremental`.** Packing keeps a small cache sidecar next to the output and reuses the compression of any cluster whose bytes have not changed since the last pack. Compressing clusters with zstd is the dominant cost of packing a large mirror, so re-packing after a small change (a `--refresh`, a handful of edited pages) only compresses what actually changed. A reused cluster is byte-for-byte what a fresh compression produces, so the archive stays deterministic.
- **Identical pages are stored once.** When a rendered page's bytes match a page already written, kage stores it as a hard link to the first copy instead of a second full file, collapsing the duplicate content a faceted site spawns when many `?q=…`/`?page=…` URLs render the same page. The summary reports how many were deduped.
- **Cleaner progress counting.** The live counter shows distinct URL paths as "pages" and the query-string permutations one path can spawn separately as "variants", so the number tracks the site's real size instead of being inflated by `?q=…` URLs.

## v0.2.1

Packed ZIM archives now carry the metadata Kiwix expects, so a mirror shows up in a ZIM reader's library with a title, a description, and an icon instead of as a blank entry.

- **Mandatory metadata is always written.** Every archive now gets a `Name` and a `Description` (a line derived from the host when `--description` is not given), the two fields `zimcheck` flags as missing otherwise.
- **The favicon becomes the book icon.** When the mirror has a usable icon (an `apple-touch-icon.png`, `favicon.png`, or a PNG-based `favicon.ico`), kage rescales it to a 48x48 PNG and stores it as `Illustrator_48x48@1`, which is the icon Kiwix shows for the archive in its library. A site with no usable icon is packed without one rather than with a broken image.

## v0.2.0

Double-click apps, so a packed mirror opens like a real desktop app instead of a terminal program.

- **`kage pack --app`** wraps the viewer in a double-click app with the site's favicon as its icon. The flag builds on the binary format, so it composes with `--base` (including a `webview` base) and `--icon`. On macOS that is a `.app` bundle; on Linux, with a Linux `--base`, an [AppImage](https://appimage.org)-style `.AppDir` that becomes a single `.AppImage` when `appimagetool` is installed. The icon is pulled from the mirror automatically, or set with `--icon`.
- **A GUI-subsystem Windows base** ships in the release as `kage_<version>_windows-gui_<arch>.zip`. Pack a viewer onto it with `--format binary --base` and the resulting `.exe` opens with no console window behind it.
- **Smarter cross-platform packing.** kage reads the base binary's executable header to detect its target OS, so a Windows viewer always gets a `.exe` name and the right run hint, regardless of how the base file is named.

## v0.1.2

A security fix for how kage launches Chrome, clearer crawl errors, and a container image that actually runs.

- **Chrome keeps its sandbox on by default.** Earlier versions launched Chrome with `--no-sandbox` on every run, which switched off the browser's main security boundary even on an ordinary desktop where the sandbox works fine ([#10](https://github.com/tamnd/kage/issues/10)). The sandbox now stays on, and is dropped only where it genuinely cannot start: inside a container (detected from `IN_DOCKER` or `/.dockerenv`) or when running as root. Whenever it is dropped, kage says so on stderr, so the choice is never silent.
- **Transient asset failures retry.** A download that hits a 403/429, a 5xx, or a network blip is retried with a short backoff, which recovers files that bot-protection rejects on the first request of a burst. Permanent failures like a 404 are not retried.
- **Clearer crawl errors.** Each failure now logs a classified reason (`HTTP 403 Forbidden`, `timed out`, ...), the URL, and the page that referenced it, and the end-of-run summary lists what went wrong instead of printing only a count.
- **The container image runs.** Chrome aborted in the image with `chrome_crashpad_handler: --database is required`, so the crash reporter is now disabled inside a container, and the `kage` user has a writable home (the mounted `/out` volume) so output, resume state, and Chrome's profile no longer fail with a permission error ([#7](https://github.com/tamnd/kage/issues/7)).

## v0.1.1

Packing, so a clone can travel as one file instead of a folder.

- **`kage pack <mirror-dir>`** collapses a mirror into a single distributable file. `--format zim` (the default) writes an open ZIM archive, the same format [Kiwix](https://kiwix.org) uses, so the file opens in any ZIM reader and not just kage. `--format binary` appends that archive to a copy of kage to make a self-contained executable that serves the site offline when run. Packing is deterministic, so the same mirror produces a byte-identical file.
- **`kage open <file.zim>`** serves a packed ZIM back over a local HTTP server, the read side of `kage pack --format zim`.
- **An optional native-window viewer.** Built with `-tags webview`, `kage open` and a packed binary show the site in a real window backed by the operating system's WebView instead of a browser tab. The default build stays pure Go and opens the browser, so the release pipeline is unchanged.
- **A pure-Go `zim` package** that reads and writes the ZIM format: a fixed header, MIME and pointer lists, zstd or stored clusters, redirects, and a trailing MD5.

## v0.1.0

The first release. kage clones a live website into a self-contained folder you can browse offline, with every script stripped out.

- **`kage clone <url>`** renders each page in headless Chrome, strips all JavaScript, and localises CSS, images, and fonts to relative paths.
- **`kage serve [dir]`** previews a cloned folder over a local file server.
- **Idempotent and resumable.** Each page is keyed by the file it writes, so a page reached over http and https, or as `/index.html` versus `/`, is fetched once. Re-running resumes; `--refresh` re-renders in place; `--force` starts clean.
- **Polite by default.** Honours `robots.txt`, seeds from `sitemap.xml`, scopes to the seed host, and runs three parallel worker tiers.
- **Packaged everywhere.** Archives, `.deb`/`.rpm`/`.apk`, a multi-arch GHCR image with Chromium bundled, checksums, SBOMs, and a cosign signature.
