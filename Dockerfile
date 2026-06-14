FROM golang:1.25 AS builder

WORKDIR /app

COPY . .
RUN make build

FROM scratch

COPY --from=builder /app/bin/terraplane /usr/local/bin/terraplane
ENTRYPOINT ["/usr/local/bin/terraplane"]