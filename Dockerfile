# scribe api -- multi-stage build.
#
# Two stages. The first has a Go compiler, git, and a package manager. The
# second has the binary and almost nothing else. Only the second one ships.
#
# That matters more than image size: a shell in a running container is a tool
# for whoever gets in. There isn't one here.

# ---------- stage 1: build ----------
#
# Pinned by digest, not by tag. Tags move -- "1.27.1-alpine" can be rebuilt and
# point somewhere new. A digest is the image, permanently.
FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build

WORKDIR /src

# Copy the dependency files on their own first, before the source.
#
# Docker caches each step. If only your Go code changed, this layer is reused
# and the download is skipped. Copy everything at once and every edit
# re-downloads the world.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 is the important flag. It builds a binary that needs no C
# libraries at all -- no libc, nothing. That is what lets the runtime stage be
# almost empty. With CGO on, the binary would look for libraries that are not
# there and refuse to start.
#
# -trimpath removes build machine paths from the binary, so the same source
# gives the same bytes on any machine.
#
# -ldflags "-s -w" drops the debug symbol table. Smaller binary; you lose
# symbol names in stack traces, which is a real trade.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# ---------- stage 2: runtime ----------
#
# distroless/static has no shell, no package manager, no busybox. It contains
# CA certificates (needed to make HTTPS calls later), timezone data, and a
# passwd file with a nonroot user.
#
# `scratch` would be even smaller but has no CA certificates, so every HTTPS
# request would fail with a confusing certificate error. Two megabytes is
# cheap insurance.
FROM gcr.io/distroless/static:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7

# Who this image says it is. Kubernetes and GitHub both read these.
LABEL org.opencontainers.image.source="https://github.com/nazmur96/scribe" \
      org.opencontainers.image.description="scribe api" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/api /api

# Numeric, not a name. Kubernetes' runAsNonRoot check reads the number; given
# a name it cannot tell whether the user is root and refuses to start the pod.
# 65532 is distroless' nonroot user.
USER 65532:65532

EXPOSE 8080

# No shell exists, so this must be the binary directly -- there is no /bin/sh
# to wrap it. That is also why there is no HEALTHCHECK instruction: it would
# need a shell or curl, and neither is here. Kubernetes probes the HTTP
# endpoint instead, which is better anyway.
ENTRYPOINT ["/api"]
