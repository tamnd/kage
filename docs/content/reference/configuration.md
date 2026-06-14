---
title: "Configuration"
description: "Environment variables kage reads, and the layout of a cloned mirror on disk."
weight: 20
---

kage is configured almost entirely through command-line flags (see the
[CLI reference](/reference/cli/)). It reads a couple of environment variables for
locating the browser.

## Environment variables

| Variable | Meaning |
|----------|---------|
| `KAGE_CHROME` | Path to the Chrome/Chromium binary. Takes precedence over autodetection. Equivalent to `--chrome`. |
| `CHROME_BIN` | Fallback Chrome path, read if `KAGE_CHROME` is unset. |

If neither is set and no system Chrome is found in the usual install locations,
kage's launcher can download a private copy of Chromium on first use.

## Output layout

A clone of `example.com` lands under `$HOME/data/kage/example.com/` (override the
root with `-o/--out`):

```
$HOME/data/kage/example.com/
├── index.html                  # the home page (/), scripts stripped
├── about/index.html            # /about
├── blog/
│   ├── index.html              # /blog
│   └── a-post/index.html       # /blog/a-post
├── _kage/                      # reserved directory
│   ├── example.com/
│   │   ├── site.css            # localised stylesheet, url() rewritten
│   │   ├── logo.png
│   │   └── fonts/body.woff2
│   ├── cdn.example.com/        # assets from other hosts, by host
│   └── state.json              # visited set, for --resume
└── ...
```

Key points:

- **Pages become directories.** A page at `/about` is written as
  `about/index.html`, so a link to `/about` resolves to a real file when served.
- **Assets live under the reserved directory.** Everything kage downloads, CSS,
  images, fonts, media, goes under `_kage/<asset-host>/`, mirroring the path it
  had on its origin. Cross-origin assets are grouped by their own host.
- **Query strings are folded into the filename.** An asset like
  `style.css?v=3` is saved with a short hash suffix so two versions never
  collide.
- **State lives in the mirror.** `_kage/state.json` records every page written,
  which is what lets a repeated run skip completed work. Rename the reserved
  directory with `--reserved` if `_kage` would clash with a real path on the site.

## Resume, refresh, and re-crawl

A clone is idempotent: every page is keyed by the file it writes, so the same
page reached over `http` and `https`, with or without a trailing slash, or as
`/index.html` versus `/`, is fetched exactly once. Re-running picks the work back
up rather than starting over.

| You want to… | Use | What happens |
|--------------|-----|--------------|
| Continue an interrupted crawl | *(default)* | Loads `state.json`, skips pages already written, fetches only what is missing |
| Pull in content that changed on the site | `--refresh` | Keeps the mirror, re-renders every page in place, overwrites with the new DOM |
| Start completely clean | `--force` | Deletes the host's mirror, then crawls from scratch |
| Run once and leave no trace | `--no-resume` | Skips nothing, writes no `state.json` |
