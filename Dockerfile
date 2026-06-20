# Consumed by GoReleaser: it copies the already cross-compiled binary out of the
# build context rather than compiling, so the image build is fast and uses the
# same static binary every other artifact ships.
#
# kage always drives a real headless Chrome, so unlike a plain CLI image this one
# bundles Chromium. KAGE_CHROME points kage at the system binary so it never
# tries to download its own.
#
# GoReleaser builds one multi-platform image with buildx and stages each
# platform's binary under a $TARGETPLATFORM directory (e.g. linux/amd64/) in the
# build context, so the COPY line selects the right one through the automatic
# TARGETPLATFORM build arg.
FROM alpine:3.21

ARG TARGETPLATFORM

# chromium for rendering; ca-certificates for HTTPS; tzdata for sane timestamps;
# the font package so rendered pages have glyphs to lay out.
RUN apk add --no-cache chromium ca-certificates tzdata font-noto \
 && mkdir -p /out

COPY $TARGETPLATFORM/kage /usr/bin/kage

WORKDIR /out

# Point kage at the bundled Chromium and write mirrors under /out by default:
#
#   docker run -v "$PWD/out:/out" ghcr.io/tamnd/kage clone example.com
#
# The container runs as root, and that is deliberate (issue #7). A bind-mounted
# /out is owned by whoever created it on the host, so only root can reliably
# write into it; a fixed non-root uid cannot, and both kage's output and resume
# state (under $HOME/data/kage) then fail with "mkdir /out: permission denied".
# The same unwritable HOME also breaks Chrome: it launches chrome_crashpad_handler
# with an empty crash database path, which aborts the whole browser with
# "chrome_crashpad_handler: --database is required" and fails every render.
# Running as root keeps /out and HOME writable whatever the host owns, so the
# one-liner above just works. This costs nothing in the sandbox: Chrome's sandbox
# is already off inside any container (kage drops it on container detection), so
# root here does not loosen a boundary that was holding. HOME points at /out so
# the default output and Chrome's writable state both land in the mounted volume.
ENV KAGE_CHROME=/usr/bin/chromium-browser \
    HOME=/out

VOLUME ["/out"]

ENTRYPOINT ["/usr/bin/kage"]
