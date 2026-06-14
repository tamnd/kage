# kage

**kage** (影, "shadow") clones a website into a self-contained folder you can
browse offline, with all the JavaScript stripped out. It renders every page in
headless Chrome, snapshots the final rendered DOM, removes every script and
event handler, and downloads the CSS, images, and fonts and rewrites them to
local paths. The result looks like the live site but runs no code: a plain
folder of `.html` files you can open straight from disk.

```bash
kage clone example.com
kage serve kage-out/example.com
```

## Why

Saving a page with "Save As" gives you a copy that still phones home, still runs
analytics, and often renders blank because the markup is built by JavaScript at
runtime. kage takes the opposite approach:

- **Render first, save second.** Each page goes through real headless Chrome, so
  a page whose content is assembled by JavaScript is captured the way a human
  would have seen it, not as an empty shell.
- **Strip every script.** Once the DOM is captured, kage removes all `<script>`
  tags, every `on*` event handler, and any `javascript:` URL. The saved page is
  inert: no tracking, no network calls, no surprises.
- **Keep the layout.** Stylesheets, images, fonts, and media are downloaded and
  rewritten to relative local paths, so the offline copy looks like the original.
- **Stay browsable.** In-scope links are rewritten to point at the other saved
  pages, so you can click around the mirror exactly as you would the live site.

## Install

```bash
# Go
go install github.com/tamnd/kage/cmd/kage@latest

# Homebrew (once the tap is published)
brew install tamnd/tap/kage

# Container (Chromium bundled)
docker run -v "$PWD/out:/out" ghcr.io/tamnd/kage clone example.com
```

Prebuilt archives, `.deb`/`.rpm`/`.apk` packages, and a multi-arch image are
attached to each [release](https://github.com/tamnd/kage/releases).

kage drives a real browser, so it needs Chrome or Chromium available. It finds a
system install automatically; point it at a specific binary with `--chrome` or
the `KAGE_CHROME` environment variable. The container image bundles Chromium.

## Usage

```bash
kage clone <url> [flags]
kage serve [dir] [flags]
kage pack <mirror-dir> [flags]
kage open <file.zim> [flags]
```

### Clone

```bash
# Clone a whole site into kage-out/<host>/
kage clone https://example.com

# Limit the crawl
kage clone example.com --max-pages 200 --max-depth 3

# Only a section of the site
kage clone example.com --scope-prefix /docs

# Include subdomains, and trigger lazy-loaded images by scrolling
kage clone example.com --subdomains --scroll

# Resume an interrupted run (on by default; Ctrl-C saves state)
kage clone example.com

# Re-render every page in place to pull in changed content
kage clone example.com --refresh
```

A clone is idempotent: each page is keyed by the file it writes, so the same URL
reached over http and https, with or without a trailing slash, is fetched once.
Re-running resumes where it left off; `--refresh` re-renders in place, `--force`
wipes and starts clean.

Common flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-o, --out` | `$HOME/data/kage` | Output root; the mirror lands in `<out>/<host>/` |
| `-p, --max-pages` | `0` | Stop after N pages (0 = unlimited) |
| `-d, --max-depth` | `0` | Link-follow depth cap (0 = unlimited) |
| `--scope-prefix` | | Only crawl pages whose path starts with this prefix |
| `--subdomains` | `false` | Treat subdomains of the seed host as in scope |
| `--exclude` | | Path prefixes to skip (repeatable) |
| `--scroll` | `false` | Auto-scroll each page to trigger lazy loading |
| `--workers` | `4` | Concurrent page render workers |
| `--no-robots` | `false` | Ignore `robots.txt` (be polite) |
| `-f, --force` | `false` | Delete any existing mirror for the host first |
| `--chrome` | | Path to the Chrome/Chromium binary |

Run `kage clone --help` for the full list.

### Serve

`kage serve` runs a local static file server over a cloned folder so links and
assets resolve the way they would on a real host:

```bash
kage serve kage-out/example.com
# open http://127.0.0.1:8800
```

### Pack

`kage pack` collapses a mirror into one distributable file. The default is an
open ZIM archive (the format Kiwix uses); `--format binary` produces a
self-contained executable that serves the site offline when run.

```bash
# A ZIM archive, browsable with kage open or any ZIM reader
kage pack kage-out/example.com
kage open example.com.zim

# A single executable that is the site
kage pack kage-out/example.com --format binary
./example
```

Packing is deterministic: the same mirror produces a byte-identical archive. The
ZIM holds the whole mirror with text zstd-compressed and media stored as-is, so
it is one tidy file to move, checksum, or hand to someone. The binary carries a
full kage, so the recipient needs nothing installed.

## How it works

```
seed URL ─▶ headless Chrome ─▶ final DOM ─▶ strip JS ─▶ localise assets ─▶ disk
              (render)          (snapshot)   (sanitize)   (rewrite links)
```

A clone is a polite breadth-first crawl. Pages are rendered by a pool of Chrome
tabs; assets are fetched over plain HTTP by a separate worker pool. Every URL
maps deterministically to a local path, so links can be rewritten before the
asset they point at has even finished downloading. The crawl honours
`robots.txt` and seeds itself from `sitemap.xml` by default. Output layout:

```
kage-out/example.com/
├── index.html                 # the home page, scripts stripped
├── about/index.html           # /about
├── _kage/                      # reserved: assets and crawl state
│   ├── example.com/site.css    # localised stylesheet (url() rewritten)
│   ├── example.com/logo.png
│   └── state.json              # visited set, for --resume
└── ...
```

## Building from source

```bash
git clone https://github.com/tamnd/kage
cd kage
make build          # -> bin/kage
make test           # full suite, including Chrome-driven end-to-end tests
make test-short     # skip the tests that launch a browser
```

By default kage is pure Go (`CGO_ENABLED=0`) and a packed binary opens the system
browser. Build with the `webview` tag for a native-window viewer that shows a
packed site in its own window, backed by the OS WebView, instead of a browser
tab:

```bash
CGO_ENABLED=1 go build -tags webview -o bin/kage ./cmd/kage
```

## License

MIT. See [LICENSE](LICENSE).
