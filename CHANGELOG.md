# Changelog

All notable changes to kage are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Identical pages are now stored once. When a rendered page's bytes match a page
  already written, kage stores it as a hard link to the first copy instead of a
  second full file. This collapses the duplicate content a faceted site spawns
  when many `?q=…`/`?page=…` URLs all render the same page. The final summary
  reports how many pages were deduped this way.

### Changed

- The progress line now counts real pages. "pages" is the number of distinct URL
  paths written, and the query-string variants that one path can spawn by the
  thousand are shown separately as "variants", so the live counter tracks the
  site's real size instead of being inflated by `?q=…` permutations.

## [0.2.1] - 2026-06-15

### Added

- ZIM archives now carry the metadata Kiwix and `zimcheck` treat as mandatory. Every archive gets a `Name` and a `Description` (a host-derived line when `--description` is not given), and, when the mirror has a usable favicon, an `Illustrator_48x48@1` entry: the icon rescaled to a 48x48 PNG, which is the book icon Kiwix shows in its library.

## [0.2.0] - 2026-06-15

### Added

- `kage pack --app` wraps the packed viewer in a double-click desktop app with
  the site's favicon as the icon. The flag builds on the binary format, so it
  composes with `--base` (including a `webview` base) and `--icon`. On macOS it
  writes a `.app` bundle (`Info.plist`, the viewer under `Contents/MacOS`, and an
  `.icns` generated from the favicon); on Linux, with a Linux `--base`, it writes
  an AppImage-style `.AppDir` and folds it into a single `.AppImage` when
  `appimagetool` is installed. The icon is found in the mirror automatically
  (preferring a large `apple-touch-icon.png`, then `favicon.png` or a PNG-based
  `favicon.ico`) and can be overridden with `--icon`.
- The release now ships a GUI-subsystem Windows base,
  `kage_<version>_windows-gui_<arch>.zip`. Packing a viewer onto it with
  `--format binary --base` produces a `.exe` that opens with no console window
  behind it, the Windows equivalent of the `.app` double-click experience.

### Changed

- Cross-platform packing detects the base binary's target OS from its executable
  header (ELF, PE, or Mach-O) rather than its file name, so a Windows viewer
  always gets a `.exe` suffix and the run hint names the right platform even when
  the base is named without one.

## [0.1.2] - 2026-06-15

### Security

- Chrome now keeps its sandbox on by default. It was previously launched with `--no-sandbox` unconditionally, which removed Chrome's main line of defense when rendering pages from the open web (reported in #10). The sandbox is now dropped only where it genuinely cannot run: inside a container, or when running as root, and the choice is logged so it is never silent.

### Added

- Container-aware Chrome flags. kage detects a container from the `IN_DOCKER` environment variable or a `/.dockerenv` marker and, only there, drops the sandbox and adds `--disable-dev-shm-usage` (the default 64 MB `/dev/shm` is too small for Chrome on large pages). Outside a container the faster shared memory is left in place.
- Asset downloads retry on a transient failure (a 403/429, a 5xx, or a network blip) with a short backoff, recovering files that bot-protection rejects on the first request of a burst. Permanent failures (404, 401, ...) are not retried.

### Changed

- Clearer crawl error reporting. Each failure is logged with a classified reason (`HTTP 403 Forbidden`, `timed out`, ...), the URL, and the page that referenced it, and the end-of-run summary lists what went wrong instead of printing only a count.

### Fixed

- The container image now runs. Chrome aborted on launch with `chrome_crashpad_handler: --database is required`, so kage disables Chrome's crash reporter inside a container, and the `kage` user now has a writable home (the mounted `/out` volume) so the default output, resume state, and Chrome's profile no longer fail with a permission error (issue #7).

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

[Unreleased]: https://github.com/tamnd/kage/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/tamnd/kage/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/tamnd/kage/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/tamnd/kage/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/tamnd/kage/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/tamnd/kage/releases/tag/v0.1.0
