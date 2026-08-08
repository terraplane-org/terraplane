# syntax=docker/dockerfile:1

FROM golang:1.25.12-bookworm AS builder

WORKDIR /src

# Community edition is enough for migrate status/apply and is ~90MB smaller
# than the proprietary atlas binary. Fetch before COPY . so code changes
# do not invalidate this layer.
ARG ATLAS_VERSION=v1.2.0
RUN mkdir -p /out \
    && curl -sSfL "https://release.ariga.io/atlas/atlas-community-linux-amd64-${ATLAS_VERSION}" \
      -o /out/atlas \
    && chmod +x /out/atlas

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/terraplane .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/terraplane /usr/local/bin/terraplane
COPY --from=builder /out/atlas /usr/local/bin/atlas

ENTRYPOINT ["terraplane"]
