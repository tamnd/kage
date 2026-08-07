---
title: "robots.txt"
description: "How site owners can control kage crawls, and when users can override those rules."
weight: 30
---

kage reads `/robots.txt` before crawling and follows the group for its `kage`
agent token by default. Agent names are matched case-insensitively, so either of
these forms applies:

```text
User-agent: kage
Disallow: /
```

```text
User-agent: Kage
Disallow: /
```

`Allow` and `Disallow` select which page paths kage may render. A disallowed
page is skipped rather than saved for a later resumed run.

## Crawl delay

kage also honours the selected group's `Crawl-delay` value, spacing page-render
starts by that duration:

```text
User-agent: kage
Crawl-delay: 2
```

A person running the crawl can provide an explicit delay instead. This value
takes precedence over the file:

```bash
kage clone example.com --crawl-delay 5s
```

## Advisory, not enforcement

robots.txt expresses a site's crawling preference; it is not access control.
kage users can bypass it with `--no-robots`:

```bash
kage clone example.com --no-robots
```

That flag skips the site's `Allow`, `Disallow`, and `Crawl-delay` rules. An
explicit `--crawl-delay` is still applied. Site owners who must prevent access
should use authentication or server-side authorization rather than relying on
robots.txt.
