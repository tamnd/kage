---
title: "CLI reference"
description: "Every kage command and flag."
weight: 10
---

```
kage [command] [flags]
```

Four commands: `clone` fetches a site into an offline folder, `serve` previews
one, `pack` collapses a mirror into a single file, and `open` serves a packed
file. Run `kage <command> --help` for the canonical, up-to-date list.

## kage clone

```
kage clone <url> [flags]
```

Renders each page in headless Chrome, strips all JavaScript, localises CSS,
images, and fonts, and writes a browsable mirror to `<out>/<host>/`.

### Output

| Flag | Default | Meaning |
|------|---------|---------|
| `-o, --out` | `$HOME/data/kage` | Output root; the mirror lands in `<out>/<host>/` |
| `--reserved` | `_kage` | Reserved directory name for assets and crawl state |
| `-f, --force` | `false` | Delete any existing mirror for the host before crawling |
| `--refresh` | `false` | Re-render every page in place to pull in changed content |
| `--no-resume` | `false` | Do not read or write resume state |

### Scope

| Flag | Default | Meaning |
|------|---------|---------|
| `-p, --max-pages` | `0` | Stop after N pages (0 = unlimited) |
| `-d, --max-depth` | `0` | Link-follow depth cap (0 = unlimited) |
| `--scope-prefix` | | Only crawl pages whose path starts with this prefix |
| `--subdomains` | `false` | Treat subdomains of the seed host as in scope |
| `--exclude` | | Path prefixes to skip (repeatable) |
| `--traversal` | `bfs` | Frontier order: `bfs` or `dfs` |

### Politeness

| Flag | Default | Meaning |
|------|---------|---------|
| `--no-robots` | `false` | Ignore `robots.txt` |
| `--no-sitemap` | `false` | Do not seed URLs from `sitemap.xml` |
| `--user-agent` | Chrome UA | User-Agent for asset and robots fetches |

### Rendering

| Flag | Default | Meaning |
|------|---------|---------|
| `--scroll` | `false` | Auto-scroll each page to trigger lazy loading |
| `--settle` | `1.5s` | Network-idle quiet period before snapshotting the DOM |
| `--render-timeout` | `30s` | Hard cap per page render |
| `--headful` | `false` | Run Chrome with a visible window (debugging) |
| `--chrome` | | Path to the Chrome/Chromium binary |
| `--control-url` | | Attach to an existing Chrome DevTools endpoint |
| `--keep-noscript` | `false` | Unwrap `<noscript>` content instead of dropping it |

### Concurrency and limits

| Flag | Default | Meaning |
|------|---------|---------|
| `--workers` | `4` | Concurrent page render workers |
| `--asset-workers` | `8` | Concurrent asset download workers |
| `--browser-pages` | `4` | Chrome page-pool size |
| `--timeout` | `30s` | Per-request timeout |
| `-q, --quiet` | `false` | Suppress per-page progress lines |

## kage serve

```
kage serve [dir] [flags]
```

Runs a local static file server over a cloned folder. With no `dir`, serves the
current directory.

| Flag | Default | Meaning |
|------|---------|---------|
| `-a, --addr` | `127.0.0.1:8800` | Address to listen on |

## kage pack

```
kage pack <mirror-dir> [flags]
```

Packs a cloned mirror into one distributable file: an open ZIM archive, or a
self-contained executable that serves the site offline when run. A bare host name
is resolved against the default output directory, so `kage pack example.com`
works right after `kage clone example.com`.

| Flag | Default | Meaning |
|------|---------|---------|
| `--format` | `zim` | Output format: `zim` or `binary` |
| `-o, --out` | per format | Output path; `<host>.zim` for zim, `<host>` (or `<host>.exe`) for binary |
| `--base` | this kage | Base kage binary to append to (`--format binary`); point at another platform's binary to build a viewer for it |
| `--app` | `false` | Wrap the viewer in a double-click desktop app (`.app` on macOS, `.AppImage`/`.AppDir` on Linux) with the site's favicon as the icon |
| `--icon` | site favicon | Icon file for `--app`, overriding the favicon found in the mirror |
| `--no-compress` | `false` | Store every cluster raw, no zstd |
| `--title` | main page `<title>` | Archive title |
| `--description` | host-derived line | Archive description (mandatory metadata, defaulted when unset) |
| `--language` | `eng` | Archive language code |
| `--date` | today | Archive date (`YYYY-MM-DD`); pass a fixed value for a reproducible file |

## kage open

```
kage open <file.zim> [flags]
```

Serves a packed ZIM over a local HTTP server for offline reading, the read side
of `kage pack --format zim`.

| Flag | Default | Meaning |
|------|---------|---------|
| `-a, --addr` | `127.0.0.1:8800` | Address to listen on |
| `--open` | `true` | Open the default browser (`--open=false` to skip) |

Built with `-tags webview` (which needs cgo), `kage open` shows the archive in a
native window instead of the browser, and `--open` no longer applies. The default
`CGO_ENABLED=0` build uses the browser.
