# Contributing to kage

Thanks for helping. kage is a small, opinionated tool: patches that stay focused
and come with tests land fastest.

## Development setup

You need a recent Go toolchain (the `go` version in `go.mod`; Go 1.21+ will
download it automatically) and, for browser tests, Chrome or Chromium.

```bash
git clone https://github.com/tamnd/kage
cd kage
make build          # -> bin/kage
make test-short     # unit tests, no Chrome
make test           # full suite, including Chrome e2e when a browser is present
make vet
```

Point kage at a browser with `KAGE_CHROME` / `CHROME_BIN` or `--chrome` if it is
not on the default path.

## What makes a good PR

- **One concern per PR.** Fix install, or sanitize, or packing — not all three
  unless they are inseparable.
- **Tests for the regression.** Prefer a small unit test over a full site clone.
  Chrome-driven tests should skip under `-short` and when no browser is found.
- **Update `CHANGELOG.md`** under `[Unreleased]` when the change is user-visible.
- **Match the local style.** Packages are deep modules with package comments;
  exported behaviour is described in prose, not only in names. Run `gofmt -s`.
- **No new dependencies** unless they remove more code than they add. Explain
  why a new module belongs in kage and what local code it replaces.

## Suggested first contributions

Open issues are the best map. High-impact areas that have already bitten users:

- SPA / lazy-content wait strategies for sites that never finish loading.
- Windows CI for `-short` tests.

Please open an issue before large design changes so the approach can be agreed.

## Reporting bugs

Include the kage version (`kage version`), OS, the exact command, and a minimal
reproducing URL when possible. Logs from a failed page render are especially
useful.

## License

By contributing, you agree that your contributions are licensed under the MIT
License (see [LICENSE](LICENSE)).
