# syntax=docker/dockerfile:1

FROM golang:1.25.12-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/terraplane .

# Community edition is enough for migrate status/apply and is ~90MB smaller
# than the proprietary atlas binary.
ARG ATLAS_VERSION=v1.2.0
RUN curl -sSfL "https://release.ariga.io/atlas/atlas-community-linux-amd64-${ATLAS_VERSION}" \
      -o /out/atlas \
    && chmod +x /out/atlas

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
