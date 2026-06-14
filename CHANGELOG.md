# Changelog

All notable changes to kage are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.1] - 2026-06-14

### Added

- `kage pack <mirror-dir>` packs a cloned folder into one distributable file.
  `--format zim` (the default) writes an open ZIM archive, the same single-file
  format Kiwix uses; `--format binary` appends that archive to a copy of kage to
  produce a self-contained executable that serves the site offline when run.
  Flags cover the output path, metadata (`--title`, `--description`,
  `--language`, `--date`), a `--base` binary for cross-platform viewers, and
  `--no-compress`.
- `kage open <file.zim>` serves a packed ZIM over a local HTTP server and opens
  your browser, the read side of `kage pack --format zim`.
- An optional native-window viewer. Built with `-tags webview` (which needs
  cgo), `kage open` and a packed binary present the offline site in a real
  window backed by the operating system's WebView (WKWebView, WebView2,
  WebKitGTK) instead of a browser tab, so a packed kage feels like a standalone
  app. The default build stays pure Go (`CGO_ENABLED=0`) and falls back to the
  system browser, so the release pipeline is unchanged.
- A pure-Go `zim` package that writes and reads the ZIM format: a fixed header,
  MIME and pointer lists, zstd-compressed (or stored) clusters, redirects, and a
  trailing MD5. It reads xz clusters so archives from other tooling open, and
  writes zstd or stored only. Packing is deterministic: the same mirror produces
  a byte-identical archive, with the UUID derived from the content rather than
  randomised.

## [0.1.0] - 2026-06-14

The first release. kage clones a live website into a self-contained folder you
can browse offline, with every script stripped out.

### Added

- `kage clone <url>` renders each page in headless Chrome, snapshots the final
  DOM, removes every `<script>`, `on*` handler, and `javascript:` URL, and
  downloads the CSS, images, fonts, and media, rewriting them to local paths.
- `kage serve [dir]` runs a local static file server over a cloned folder so the
  mirror's links and assets resolve the way they would on a real host.
- Deterministic URL-to-path mapping: pages become `<slug>/index.html`
  directories, assets live under the reserved `_kage/<host>/` tree, and query
  strings fold into a short hash suffix so versioned URLs never collide.
- Three concurrency tiers run in parallel: page-render workers (`--workers`),
  asset-download workers (`--asset-workers`), and a Chrome page pool
  (`--browser-pages`).
- A polite crawl by default: honours `robots.txt`, seeds from `sitemap.xml`,
  and scopes to the seed host. `--scope-prefix`, `--max-depth`, `--max-pages`,
  `--subdomains`, and `--exclude` shape the frontier.
- Idempotent, resumable crawling. Each page is keyed by the file it writes, so
  the same URL reached over http and https, with or without a trailing slash,
  or as `/index.html` versus `/`, is fetched exactly once. A re-run resumes from
  `_kage/state.json`; `--refresh` re-renders a mirror in place to pull in
  changed content; `--force` wipes and starts clean; `--no-resume` runs
  stateless.
- Defaults to a per-user data directory (`$HOME/data/kage`), overridable with
  `-o/--out`.
- Cross-platform distribution: prebuilt archives, `.deb`/`.rpm`/`.apk` packages,
  a multi-arch container image on GHCR (Chromium bundled), checksums, SBOMs, and
  a cosign signature, all cut from one version tag by GoReleaser.

[Unreleased]: https://github.com/tamnd/kage/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/tamnd/kage/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/tamnd/kage/releases/tag/v0.1.0
