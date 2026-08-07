# Changelog

All notable changes to kage are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project aims to follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- A `robots.txt` reference explains the `kage` agent token, `Crawl-delay`, and
  the advisory `--no-robots` override ([#8](https://github.com/tamnd/kage/issues/8)).

### Fixed

- `--resume` picks an interrupted crawl back up instead of doing nothing ([#36](https://github.com/tamnd/kage/issues/36)).
  `state.json` persisted only the visited set, and the frontier was rebuilt purely by re-rendering pages and following their links, which resume exists to avoid.
  So a resumed run found its seed already visited, `enqueuePage` turned it down, nothing was queued, and the run printed `pages 0` and exited successfully with most of the site still missing.
  Only sites with a `sitemap.xml` appeared to work, because those URLs are seeded independently on every run.
  The unfinished frontier is now saved alongside the visited set, with each page's depth so `--max-depth` keeps its meaning across a restart, and re-queued at startup.
  A run now also reports what it is leaving behind: `resume: 412 pages still to do, rerun to continue`.
- A page that failed is retried by the next run instead of being lost.
  Failures were never recorded anywhere, so the only way to pick them up was `--refresh`, which re-renders the entire site.
  This is the "memory of what failed" asked for in [#36](https://github.com/tamnd/kage/issues/36).
  A page `robots.txt` disallows is not carried over, since a later run would only fetch `robots.txt` and skip it again.
- `--max-pages` no longer discards the pages it held back.
  They stay in the frontier, so `kage clone example.com -p 20` to inspect a site and then `kage clone example.com` to finish it now works as a workflow.

- `--scroll` scrolls the element that actually scrolls, so lazy-loaded content appears on app-shell pages ([#61](https://github.com/tamnd/kage/issues/61)).
  It called `window.scrollBy`, which moves nothing on a site whose body is fixed to the viewport height with the document inside an inner container, and that is how Feishu, Notion, Linear and most dashboards are built.
  The loop also stopped as soon as the distance travelled reached `document.body.scrollHeight`, which on those pages is one viewport, so it gave up after a single step even where the window did scroll.
  kage now picks the largest genuinely scrollable element on the page, reads `scrollTop` back after each step instead of assuming the step landed, and keeps going until the position and the height both stop changing, which is what infinite scroll needs.
  The scroll is bounded by half the render timeout, at least five seconds, so a page that appends content forever cannot hold a worker indefinitely.

- Saved pages keep their `<!DOCTYPE html>` instead of rendering in quirks mode ([#16](https://github.com/tamnd/kage/issues/16)).
  kage serialises a rendered page as the outerHTML of `<html>`, and a doctype is a sibling of `<html>` rather than a child, so it was never in that string and every page kage has ever written came out without one.
  A document with no doctype is quirks mode in every browser: the box model reverts to the pre-CSS2 IE one and `line-height`, table cell inheritance and `vertical-align` all change, so the saved copy laid out differently from the original, and the `<meta charset>` declaration lost its authority, leaving a reader free to fall back to its locale encoding and mojibake every multibyte character.
  That is the encoding problem reported in #16, and a webview or e-reader with no encoding menu has no way back from it.
  The doctype is now read from the DOM and reproduced exactly rather than replaced with `<!DOCTYPE html>`, because the string itself selects the rendering mode: HTML 4.01 Transitional is standards mode with its system identifier and quirks mode without it.
  A page that genuinely had no doctype on the live web still gets none, so it keeps rendering the way its author saw it.
- The `cloned by kage` banner comment is written after the doctype rather than before it, so the doctype stays the first thing in the file.

## [0.3.11] - 2026-08-01

### Fixed

- `go install github.com/tamnd/kage/cmd/kage@latest` works again ([#72](https://github.com/tamnd/kage/issues/72)).
  The v0.3.9 antivirus fix used a local `replace` directive to remove Rod's embedded leakless watchdog, but Go rejects versioned installation of any module whose dependencies are changed that way.
  Windows now uses a small native Chrome launcher that never imports leakless, while other platforms retain Rod's launcher; this removes the `replace` directive without putting the antivirus-flagged helper back into `kage.exe`.

### Security

- Updated `golang.org/x/text` to v0.39.0 for [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970), an infinite loop on invalid input.
  `kage clone` reached the affected normalization code through the progress renderer, so govulncheck reported it as callable rather than merely present.

## [0.3.10] - 2026-07-11

### Changed

- Updated the Go toolchain requirement to 1.26.5.

## [0.3.9] - 2026-07-08

### Fixed

- The Windows build no longer embeds the leakless watchdog binary that Windows Defender flags as `Trojan:Win32/Kepavll!rfn`, which made a fresh `scoop install` fail with a virus warning on `leakless.exe` ([#68](https://github.com/tamnd/kage/issues/68)).
  go-rod's launcher imports [leakless](https://github.com/ysmood/leakless), which base64/gzip-embeds a prebuilt helper for every platform and links the Windows one into `kage.exe`.
  kage already launches Chrome with leakless disabled, so the helper never ran, only added the flagged bytes.
  This release initially used a `replace` directive pointing at an API-compatible stub under `third_party/leakless`; the follow-up fix for [#72](https://github.com/tamnd/kage/issues/72) moved Windows to a launcher that does not import leakless, because Go rejects versioned installs of modules containing `replace`.

## [0.3.6] - 2026-06-19

### Fixed

- `kage clone --mobile` now produces a correct layout when the source page uses table-based navigation with image maps, as paulgraham.com does.
  Three rendering problems appeared in Kiwix iOS after the 0.3.5 release: a tall blank box at the top of every page, content clipped on the right edge, and a narrow spacer column occupying screen space.
  The blank box came from hiding only the `<img usemap>` element while leaving its `<td>` container in place; the cell collapsed to empty but kept its full height.
  `td:has(>img[usemap])` now hides the entire nav column rather than just the image inside it.
  The right-side clip came from `width="435"` set directly as an HTML attribute on the inner content table; the earlier `max-width` CSS rule does not override HTML attributes.
  A new `[width]{width:auto!important;max-width:100%!important}` rule cancels every fixed HTML width attribute on any element — tables, cells, and images alike — so the content column stretches to fill the phone screen.
  The spacer column (a 26 px `<td>` holding a 1×1 transparent GIF) kept its allocated width for the same reason; `td:has(>img[src*="trans_1x1"]:only-child)` hides it alongside the nav column.
  Two additional rules round out the fix: `*{box-sizing:border-box}` prevents padding from pushing content past the viewport edge, and `img{max-width:100%;height:auto}` keeps any inline photos within their column.

## [0.3.5] - 2026-06-19

### Changed

- Each saved page is now stored in a packed ZIM under its own `<title>` instead of its URL path, so a ZIM reader's search box suggests pages by their readable title.
  Typing into Kiwix's search now offers "Five Founders" or "Female Founders" rather than a filename, because the title pointer list a reader's suggestion search walks carries the real page titles.
  A page with no `<title>` still falls back to its path, and the per-page title also flows into the `title` column of `kage parquet export`.

### Added

- `kage clone --mobile` makes legacy "font-era" sites readable on a phone.
  Sites from the 1990s and early 2000s — paulgraham.com is a good example — embed typography directly in the HTML with `<font size="2" face="verdana">`, table-based layouts, and no viewport declaration.
  A mobile browser receiving that markup without a viewport meta shrinks everything to desktop scale, and the `<font size="2">` instruction then makes the already-small text microscopic.
  Passing `--mobile` injects two things into every saved page before it is written: a `<meta name="viewport" content="width=device-width, initial-scale=1">` tag so the browser stops shrinking, and a small `<style>` block that lifts the base font size to 18 px, inherits that size through `<font>` elements, caps the content width at 720 px, loosens line height to 1.7, and hides image-map navigation elements (usually a GIF served from an external CDN that 404s offline anyway).
  The override is deliberately last in `<head>` so it wins specificity ties, and it does not touch pages that already carry a viewport and readable type sizes.
- The packing guide now documents how search works on a kage archive: title suggestions in any ZIM reader, and full-text search of page bodies through `kage parquet export` and DuckDB.
  A Xapian full-text index is deliberately not written, since Xapian is GPL and kage is MIT.

## [0.3.4] - 2026-06-17

### Fixed

- `kage serve` now stops on Ctrl-C instead of ignoring it.
  The preview server was started with a blocking call that never watched for an interrupt, so the only way to stop it was to kill the process.
  kage now shuts the server down gracefully on an interrupt or a `SIGTERM`, with a short timeout before it forces the listener closed, so a preview exits cleanly. Thanks to Xirui Wang (#35) and Kaidi Zhao (#38).
- A page whose JavaScript builds a deeply nested object graph no longer fails to clone.
  Chrome's DevTools Protocol returns "Object reference chain is too long" while loading such a page, but the HTML has already loaded and the error is only about Chrome's internal object tracking, not the document.
  kage now recognises that specific error and finishes rendering the page instead of dropping it (reported in #36). Thanks to Gautam Kumar (#39).

## [0.3.3] - 2026-06-16

### Fixed

- Chrome no longer downloads a file to your Downloads folder when a crawl follows a link that turns out to be a binary (reported in #32).
  An extensionless link is queued as a page, so the page worker navigated to it in Chrome, and a link that served a zip or a CSV made Chrome save the file to `~/Downloads`, a surprise side effect of a clone.
  kage now denies Chrome-initiated downloads browser-wide, since every asset is fetched through kage's own downloader, and detects a navigation whose response is not HTML and reroutes that URL to the asset downloader, where the size and media policy decides whether to localise it or leave it on the live web.

## [0.3.2] - 2026-06-16

### Fixed

- Saved pages now declare their character encoding, so text no longer mojibakes in a reader.
  kage writes every page as UTF-8, but a source that set its charset only in the HTTP `Content-Type` header, with no `<meta charset>` in the markup, lost that signal once the page became a standalone file.
  A reader serving the bytes without a charset then fell back to its locale encoding and turned every curly quote, dash, and non-breaking space into mojibake (reported in #16 and #29).
  kage now inserts a `<meta charset="utf-8">` at the top of `<head>` when the page does not already declare one, so the page is self-describing in any reader.

## [0.3.1] - 2026-06-15

### Fixed

- A served mirror whose entry point is a nested page no longer loses its CSS and
  images when opened at the root. kage saves each page's asset links as
  mirror-relative paths (`../_kage/...`) computed for that page's own location,
  but the viewer answered `/` with the main page's bytes in place, so the browser
  resolved those relative URLs against `/` and missed every one. A
  `developer.apple.com/documentation` mirror, whose main page is
  `developer.apple.com/documentation/index.html`, landed at `/` completely
  unstyled. kage now redirects `/` to the main page's canonical content path, the
  way the archive's `W/mainPage` redirect already does, so the browser resolves
  the page's relative assets correctly. Kiwix was unaffected because it follows
  that redirect itself.

## [0.3.0] - 2026-06-15

### Added

- `kage parquet export <file.zim>` and `kage parquet import <file.parquet>`
  convert a packed archive to a columnar Parquet table and back. The table is
  flat, one row per archive entry, with clear columns (url, host, title, mime,
  extracted text, content), so it drops straight into the tooling a dataset host
  like Hugging Face expects, and DuckDB or pandas can query it as is. The column
  names follow the open-index/open-markdown dataset (`doc_id`, `url`, `host`,
  `crawl_date`, `content_length`, `text_length`, `text`), with `doc_id` a
  deterministic UUID v5 of the page URL, so a kage export sits alongside other
  web-crawl datasets. The conversion is lossless: a ZIM round-tripped through
  Parquet reproduces every entry, its metadata, and the main page byte for byte.
- `kage pack --incremental` keeps a small cache sidecar next to the output and
  reuses the compression of any cluster whose bytes have not changed since the
  last pack. Compressing clusters with zstd is the dominant cost of packing a
  large mirror, so re-packing after a small change (a `--refresh`, a handful of
  edited pages) only compresses what actually changed instead of the whole
  archive. A cached cluster is byte-for-byte what a fresh compression produces,
  so the archive stays deterministic and valid. The pack reports how many
  clusters it reused versus compressed.
- Identical pages are now stored once. When a rendered page's bytes match a page
  already written, kage stores it as a hard link to the first copy instead of a
  second full file. This collapses the duplicate content a faceted site spawns
  when many `?q=…`/`?page=…` URLs all render the same page. The final summary
  reports how many pages were deduped this way.

### Changed

- Clones no longer pull a site's bulk downloads into the mirror by default. Video
  and audio, installers and disk images (`.dmg`, `.pkg`, `.exe`, `.msi`, ...),
  archives, and PDFs are left pointing at their live URL instead of downloaded,
  because they are rarely needed to read a site offline yet routinely make up
  most of its bytes (a developer.apple.com crawl was 18 of 19 GB of such assets).
  Page-rendering assets (images, fonts, CSS) are unaffected. `--keep-media`
  restores the old behavior, and `--skip-ext .foo` leaves more extensions remote.
- Assets are localized only from the seed's own registrable domain by default.
  developer.apple.com still pulls from www.apple.com and images.apple.com, but a
  separate brand domain or an unrelated third party (an embedded tracker, an
  off-topic CDN) is left on the live web rather than mirrored. `--all-asset-hosts`
  restores downloading assets from any host.
- The progress line now counts real pages. "pages" is the number of distinct URL
  paths written, and the query-string variants that one path can spawn by the
  thousand are shown separately as "variants", so the live counter tracks the
  site's real size instead of being inflated by `?q=…` permutations.

### Fixed

- An asset larger than the size cap (`--max-asset-mb`, 25 by default) is now
  skipped instead of being truncated to a corrupt fragment. The cap was a
  `LimitReader`, so an over-size file was saved as exactly the first N MB of
  itself: a broken video or installer that wasted disk and would never play or
  run. kage now checks the response size and leaves an over-cap asset out of the
  mirror entirely, pointing at its live URL. On a developer.apple.com crawl this
  was around a gigabyte of truncated WWDC videos and `.dmg` installers.
- An asset URL whose query string carries a raw space is now requested with the
  space percent-encoded, so the server gets a valid request instead of rejecting
  it. Real sites bust a stylesheet's cache with a date, producing an href like
  `styles/main.css?Thursday, 26-Feb-2026 16:26:41 UTC`; a browser encodes the
  spaces before requesting, but kage was passing them through verbatim and the
  server answered `400 Bad Request`. On a developer.apple.com crawl this was the
  cause of the large majority of the download errors. The query is re-encoded on
  the canonical URL, so the on-disk key matches the fixed request.

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

[Unreleased]: https://github.com/tamnd/kage/compare/v0.3.9...HEAD
[0.3.9]: https://github.com/tamnd/kage/compare/v0.3.8...v0.3.9
[0.3.4]: https://github.com/tamnd/kage/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/tamnd/kage/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/tamnd/kage/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/tamnd/kage/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/tamnd/kage/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/tamnd/kage/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/tamnd/kage/compare/v0.1.2...v0.2.0
[0.1.2]: https://github.com/tamnd/kage/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/tamnd/kage/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/tamnd/kage/releases/tag/v0.1.0
