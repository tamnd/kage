---
title: "Resuming a run"
description: "Pick up an interrupted clone where it left off, and start fresh when you want to."
weight: 30
---

Cloning a large site can take a while, and runs get interrupted: you press
Ctrl-C, your laptop sleeps, the network drops. kage is built to pick up where it
left off.

## How resume works

As it writes each page, kage records it in a small state file inside the mirror,
at `<host>/_kage/state.json`. When a run ends, for any reason, that file holds
the set of pages already written. Resume is **on by default**: the next time you
run the same clone, kage loads the state and skips every page it already wrote,
re-crawling only what is left.

```bash
kage clone example.com
# ... press Ctrl-C partway through ...
# interrupted; resume state saved (rerun to continue)

kage clone example.com
# resume: 137 pages already done
```

Ctrl-C is a clean stop: kage cancels in-flight renders, flushes the state file,
and exits. You will not lose the pages already written.

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
