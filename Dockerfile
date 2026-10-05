# syntax=docker/dockerfile:1

# The intakt service image. Two stages: a Go builder, and a runtime that carries
# a TeX installation and nothing else of the toolchain.
#
# TeX is the whole reason this is a separate image. Rendering an intäktsräkning
# means running pdflatex, and putting a TeX distribution inside utlagg's
# node:22-alpine image would add several hundred megabytes to the thing that
# serves every page. Here it sits in a container that does one job.

# ── builder ───────────────────────────────────────────────────────────
FROM golang:1.25-bookworm AS builder
WORKDIR /src

# Manifests first, so the module download is reused on every build that did not
# change a dependency.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static: nothing here needs cgo, and a self-contained binary means the runtime
# stage owes it no shared libraries. The timezone database is compiled in (see
# report/timezone.go), so the image needs no tzdata either.
# The main package moved to the module root when the CLI was reorganised
# into internal/cli; the binary and its `serve` command are unchanged.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/intakt .

# ── runtime ───────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS runtime

# The exact TeX subset the skeletons need, resolved package by package from what
# they \usepackage rather than guessed at:
#
#   texlive-latex-base         geometry, graphicx, longtable, array, inputenc,
#                              and palatino.sty itself
#   texlive-latex-recommended  booktabs
#   texlive-fonts-recommended  the URW Palladio Type 1 fonts palatino selects
#   texlive-pictures           tikz, for the variant arrow
#
# Deliberately NOT texlive-latex-extra: it is around half a gigabyte and the
# only thing these documents wanted from it was booktabs, which is in
# -recommended. Adding a \usepackage to a skeleton may mean adding a package
# here — `kpsewhich <name>.sty` then `dpkg -S` on the result says which.
#
# curl is for the container healthcheck below, and is worth the couple of
# megabytes for being able to probe the service from inside its own network.
RUN apt-get update && apt-get install -y --no-install-recommends \
      texlive-latex-base \
      texlive-latex-recommended \
      texlive-fonts-recommended \
      texlive-pictures \
      ca-certificates \
      curl \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/intakt /usr/local/bin/intakt
# The .tex skeletons and their images. Read-only at runtime: a render copies
# nothing and writes only into its own temp directory.
COPY assets ./assets

# Unprivileged. pdflatex needs somewhere writable, which is $TMPDIR — /tmp is
# world-writable and that is all a render ever touches.
RUN useradd --system --create-home --uid 10001 intakt
USER intakt

ENV INTAKT_ADDR=:8080 \
    INTAKT_ASSETS=/app/assets

EXPOSE 8080

# /healthz needs no token, which is what makes it usable here.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["intakt"]
CMD ["serve"]
