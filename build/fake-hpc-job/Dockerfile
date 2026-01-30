FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o fake-hpc-job cmd/fake-hpc-job/main.go

FROM alpine:latest

WORKDIR /

COPY --from=builder /app/fake-hpc-job .

ENTRYPOINT ["/fake-hpc-job"]
