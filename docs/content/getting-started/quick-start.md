---
title: "Quick start"
description: "From an empty terminal to a self-contained offline mirror you can click through."
weight: 30
---

This walks the core loop: clone a small site, look at what landed on disk, and
serve it back so links and assets resolve the way they would on a real host.

## 1. Clone a site

```bash
kage clone example.com
```

kage launches headless Chrome, renders the home page, strips its scripts, and
follows in-scope links breadth-first. A live counter shows pages, assets, and
errors as it goes; the final summary tells you where the mirror landed.

```
kage cloning https://example.com
done $HOME/data/kage/example.com
  pages 12   assets 38
  open kage serve $HOME/data/kage/example.com
```

## 2. Look at what landed

```bash
ls $HOME/data/kage/example.com
```

```
index.html        # the home page, scripts stripped
about/index.html  # /about
_kage/            # localised assets and crawl state
```

Open `index.html` directly in a browser and it renders offline, with no network.
Grep it and you will find no `<script>`, no `onclick`, no `javascript:`.

## 3. Serve it back

Opening files directly works, but some sites use root-relative links. `kage
serve` runs a local static server so everything resolves exactly as it would
live:

```bash
kage serve $HOME/data/kage/example.com
# open http://127.0.0.1:8800
```

## 4. Scope a bigger crawl

For a large site, bound the crawl so it does not run away:

```bash
# Just the docs section, three levels deep, at most 200 pages
kage clone example.com --scope-prefix /docs --max-depth 3 --max-pages 200
```

If you stop a run with Ctrl-C, kage saves its state. Run the same command again
and it resumes, skipping the pages it already wrote.

## Where to go next

- The [guides](/guides/) cover scoping, serving, and resuming in depth.
- The [CLI reference](/reference/cli/) lists every flag.
