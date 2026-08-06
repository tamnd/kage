---
title: "Scoping a crawl"
description: "Keep a clone inside the lines with depth, page, prefix, subdomain, and exclude controls."
weight: 10
---

By default kage crawls every in-scope page it can reach from the seed, staying on
the seed's host. On a large site that can be a lot of pages. These flags bound
the crawl.

## Limit by count and depth

```bash
# Stop after 200 pages
kage clone example.com --max-pages 200

# Only follow links three hops from the seed
kage clone example.com --max-depth 3
```

`--max-depth 0` (the default) means unlimited depth; `--max-pages 0` means
unlimited pages. Combine them to put a hard ceiling on a run.

## Limit by path

To clone just one section of a site, restrict the crawl to a path prefix:

```bash
kage clone example.com --scope-prefix /docs
```

Only pages whose path starts with `/docs` are followed. Assets are still fetched
from wherever the page references them, so the section renders correctly.

To skip parts of a site, exclude path prefixes (repeatable):

```bash
kage clone example.com --exclude /archive --exclude /tags
```

## Subdomains

By default a clone stays on the exact seed host. To treat subdomains of the seed
as in scope, add `--subdomains`:

```bash
kage clone example.com --subdomains
```

Now `blog.example.com` and `docs.example.com` are crawled too, each landing
under its own host directory inside the mirror.

## Politeness

kage honours `robots.txt` by default and seeds itself from `sitemap.xml`. If you
are cloning a site you control, or you have a reason to ignore the robots rules,
you can turn them off, but do so responsibly:

```bash
kage clone example.com --no-robots --no-sitemap
```

## Lazy-loaded media

Sites that load images as you scroll will only have their above-the-fold images
captured unless you tell kage to scroll each page:

```bash
kage clone example.com --scroll
```

This makes each render a little slower but captures media that only loads on
view.

kage scrolls whichever element on the page actually scrolls, not just the
window, so this works on app-shell sites where the body is fixed to the viewport
and the document lives inside an inner container. It keeps stepping until both
the scroll position and the page height stop changing, which is what an infinite
feed needs, and gives up after half the render timeout so a page that appends
content forever cannot stall the crawl. Raise `--render-timeout` if a long feed
is being cut short.
