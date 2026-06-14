FROM golang:1.25.11 AS builder

WORKDIR /app

RUN apt-get update && apt-get install -y unzip

RUN wget -q https://github.com/protocolbuffers/protobuf/releases/download/v35.1/protoc-35.1-linux-x86_64.zip -O protoc.zip && \
    unzip protoc.zip -d /usr/local && \
    rm protoc.zip

COPY Makefile .
COPY proto ./proto

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest

RUN make protoc-gen

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build

FROM scratch

COPY --from=builder /app/bin/terraplane /usr/local/bin/terraplane
ENTRYPOINT ["/usr/local/bin/terraplane"]