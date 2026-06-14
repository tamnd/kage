---
title: "Release notes"
description: "What changed in each kage release."
weight: 40
---

The authoritative, commit-level history lives in [`CHANGELOG.md`](https://github.com/tamnd/kage/blob/main/CHANGELOG.md) and on the [releases page](https://github.com/tamnd/kage/releases). This page summarises each version.

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
