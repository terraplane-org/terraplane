FROM golang:1.25.12

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    git \
    openssh-client \
    && rm -rf /var/lib/apt/lists/*

RUN curl -sSfL https://release.ariga.io/atlas/atlas-linux-amd64-latest -o /usr/local/bin/atlas \
    && chmod +x /usr/local/bin/atlas

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN make build

ENTRYPOINT ["/app/bin/terraplane"]
