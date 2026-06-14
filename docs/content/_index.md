---
title: "kage"
description: "kage (影, shadow) clones any website into a self-contained folder you can browse offline, with all the JavaScript stripped out. Render in headless Chrome, remove every script, localise the CSS, images, and fonts, from one pure-Go binary."
heroTitle: "A website, frozen as a shadow"
heroLead: "kage renders every page in headless Chrome, snapshots the final DOM, removes every script and event handler, and downloads and rewrites the CSS, images, and fonts. The result looks like the live site but runs no code: a plain folder of .html files you can open straight from disk."
heroPrimaryURL: "/getting-started/quick-start/"
heroPrimaryText: "Get started"
---

Saving a page with "Save As" gives you a copy that still phones home, still runs analytics, and often renders blank because the markup is built by JavaScript at runtime. kage (影, "shadow") takes the opposite approach: it drives a real browser, captures the page the way a human would have seen it, then makes it inert.

Say you want Paul Graham's essays on a laptop with no wifi. One command mirrors the site; a second serves it back offline:

```bash
kage clone paulgraham.com
kage serve $HOME/data/kage/paulgraham.com
```

![kage cloning paulgraham.com, packing it into one file, and serving it back offline](/demo.gif)

## What it does

- **Renders first, saves second.** Each page goes through real headless Chrome, so a page whose content is assembled by JavaScript is captured fully, not as an empty shell.
- **Strips every script.** Once the DOM is captured, kage removes all `<script>` tags, every `on*` event handler, and any `javascript:` URL. The saved page makes no network calls and runs no code.
- **Keeps the layout.** Stylesheets, images, fonts, and media are downloaded and rewritten to relative local paths, so the offline copy looks like the original.
- **Stays browsable.** In-scope links are rewritten to point at the other saved pages, so you can click around the mirror exactly as you would the live site.
- **Packs into one file.** Collapse a mirror into a single [ZIM archive](/guides/packing-a-mirror/), the open format Kiwix uses, or a self-contained binary that serves the site when run.

Build kage with the `webview` tag and a packed binary opens in its own window instead of a browser tab, so an offline mirror feels like a real app:

![paulgraham.com served offline in a native kage window](/webview.png)

## Where to go next

- New here? Start with the [introduction](/getting-started/introduction/), then the [quick start](/getting-started/quick-start/).
- Want to install it? See [installation](/getting-started/installation/).
- Looking for a specific task? The [guides](/guides/) cover scoping a crawl, serving a mirror, resuming an interrupted run, and [packing a mirror](/guides/packing-a-mirror/) into one file or a self-contained viewer.
- Need every flag? The [CLI reference](/reference/cli/) is the full surface.
