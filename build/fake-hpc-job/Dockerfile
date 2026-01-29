FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build -o fake-hpc-job main.go

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/fake-hpc-job .

LABEL org.opencontainers.image.source="https://github.com/hpc-sentinel/fake-hpc-job"

ENTRYPOINT ["./fake-hpc-job"]
