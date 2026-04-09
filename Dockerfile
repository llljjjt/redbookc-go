FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install Playwright dependencies
RUN apk add --no-cache git bash && \
    wget -qO- https://dl.google.com/linux/linux_signing_key.pub | apk add --no-cache - && \
    echo "deb [signed-by=/etc/apk/keys/signing-key.gpg] https://dl.google.com/linux/chrome/pkg/current stable main" >> /etc/apk/repositories && \
    apk add --no-cache chromium

# Install Playwright for Go
RUN go install github.com/playwright-community/playwright-go@latest && \
    go install github.com/mattn/go-sqlite3@latest

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o redbookc-go ./cmd/server

# Runtime image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates chromium bash

WORKDIR /app

COPY --from=builder /app/redbookc-go .
COPY --from=builder /app/profiles ./profiles
COPY --from=builder /app/uploads ./uploads

ENV PORT=8080
ENV TOKEN_SECRET=change-me-in-production
ENV WEBHOOK_SECRET=change-me-in-production
ENV DATABASE_PATH=/app/data/redbookc.db

RUN mkdir -p /app/data /app/profiles /app/uploads

EXPOSE 8080

CMD ["./redbookc-go"]
