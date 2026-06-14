---
title: "Serving a mirror"
description: "View a cloned folder the way it would render on a real host, with kage serve."
weight: 20
---

A clone is a plain folder of files, so the simplest way to view it is to open an
`.html` file in your browser. That works for many sites. But some pages use
root-relative URLs (`/style.css`, `/img/logo.png`), which only resolve correctly
when served from the root of a host. `kage serve` gives you that root.

## Serve a clone

```bash
kage serve $HOME/data/kage/example.com
```

```
kage serve $HOME/data/kage/example.com
  open http://127.0.0.1:8800
  press Ctrl-C to stop
```

Open the printed URL and click around the mirror exactly as you would the live
site. Every in-scope link kage rewrote points at another saved page; every asset
resolves to its localised copy.

## Choose an address

By default kage serves on `127.0.0.1:8800`. Change it with `--addr`:

```bash
# A different port
kage serve $HOME/data/kage/example.com --addr 127.0.0.1:9000

# Reachable from other machines on your network (be deliberate about this)
kage serve $HOME/data/kage/example.com --addr 0.0.0.0:8800
```

## Serve the current directory

With no argument, `kage serve` serves the current directory, which is handy from
inside an output folder:

```bash
cd $HOME/data/kage/example.com
kage serve
```
