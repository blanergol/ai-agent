FROM golang:1.25.1-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/agent-core ./cmd/agent-core

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app

COPY --from=builder /out/agent-core /app/agent-core

RUN mkdir -p /var/log/agent /app/state

EXPOSE 8080

ENTRYPOINT ["/app/agent-core"]
CMD ["serve", "--addr", ":8080", "--first-only=false"]
