---
title: "Resuming a run"
description: "Pick up an interrupted clone where it left off, and start fresh when you want to."
weight: 30
---

Cloning a large site can take a while, and runs get interrupted: you press
Ctrl-C, your laptop sleeps, the network drops. kage is built to pick up where it
left off.

## How resume works

kage keeps a small state file inside the mirror, at `<host>/_kage/state.json`.
When a run ends, for any reason, it holds two things: the pages already written,
and the frontier that was still outstanding. Resume is **on by default**: the
next time you run the same clone, kage skips every page it already wrote and
picks the frontier back up where it stopped.

```bash
kage clone example.com
# ... press Ctrl-C partway through ...
# resume: 412 pages still to do, rerun to continue
# interrupted; resume state saved (rerun to continue)

kage clone example.com
# resume: 137 pages already done
# resume: picking up 412 pages
```

Ctrl-C is a clean stop: kage cancels in-flight renders, flushes the state file,
and exits. You will not lose the pages already written.

## Pages that failed are tried again

A page that errored, a render timeout, a host that was briefly down, is never
recorded as written, so it stays in the frontier and the next run tries it
again. Running the same clone a second time after a flaky network is enough to
fill in the gaps.

A page that `robots.txt` disallows is not carried over, since a later run would
only fetch `robots.txt` and skip it again.

## Raising a page budget

`--max-pages` is a per-run budget, and the pages it held back stay in the
frontier. That makes it a way to look before you leap:

```bash
kage clone example.com -p 20     # see what the first 20 pages look like
kage clone example.com           # happy with it, crawl the rest
```

## Start fresh

To ignore any previous run and rebuild the mirror from scratch, delete the
existing host folder first with `--force`:

```bash
kage clone example.com --force
```

This removes `$HOME/data/kage/example.com/` before crawling, so nothing from a prior run
carries over.

To run without reading or writing any resume state at all, for a strictly
one-shot clone, use `--no-resume`:

```bash
kage clone example.com --no-resume
```
