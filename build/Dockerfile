# ================================================================
# doc_flow_agent build image (ubuntu:24.04 + Go toolchain).
#
# Go is NOT downloaded inside the image — the tarball is cached on
# the host and COPY'd in at build time (see build.sh).
#
# Module cache is volume-mapped from the host at run time, so deps
# are never re-downloaded inside the container.
# ================================================================
FROM ubuntu:24.04

ARG HTTP_PROXY=""
ARG HTTPS_PROXY=""
ARG NO_PROXY=""
ENV HTTP_PROXY="$HTTP_PROXY" HTTPS_PROXY="$HTTPS_PROXY" NO_PROXY="$NO_PROXY" \
    http_proxy="$HTTP_PROXY" https_proxy="$HTTPS_PROXY" no_proxy="$NO_PROXY"

RUN if [ -n "$HTTP_PROXY" ]; then \
        echo "Acquire::http::Proxy \"$HTTP_PROXY\";" > /etc/apt/apt.conf.d/01proxy; \
    fi; \
    if [ -n "$HTTPS_PROXY" ]; then \
        echo "Acquire::https::Proxy \"$HTTPS_PROXY\";" >> /etc/apt/apt.conf.d/01proxy; \
    fi

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates gcc g++ libc6-dev pkg-config git \
    gcc-mingw-w64-x86-64 \
    && rm -rf /var/lib/apt/lists/*

# ── Go toolchain (from host-cached tarball) ──────────────────────
ARG GO_VERSION=1.24.13
COPY dist/cache/deps/go${GO_VERSION}.linux-amd64.tar.gz /tmp/go.tar.gz
RUN tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOTOOLCHAIN=local
ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /workspace