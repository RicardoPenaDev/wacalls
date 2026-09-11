# Stage 1: Build the React frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/client
COPY client/package*.json ./
RUN npm install
COPY client/ ./
RUN npm run build

# Stage 2: Build the Go backend
FROM golang:alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/wacalls-server ./cmd/server

# Stage 3: Minimal runtime
FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata ffmpeg

WORKDIR /app
COPY --from=backend-builder /app/wacalls-server /app/wacalls-server
COPY --from=frontend-builder /app/dist /app/dist
COPY entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/wacalls-server /app/entrypoint.sh && \
    mkdir -p /data/media

ENV TZ=America/Sao_Paulo
EXPOSE 8080

VOLUME ["/data"]

ENTRYPOINT ["/app/entrypoint.sh"]