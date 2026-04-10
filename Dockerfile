# Stage 1: Build
FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.Version=${VERSION}" -o ysf-reflector .

# Stage 2: Runtime
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /app/ysf-reflector /ysf-reflector

USER nonroot:nonroot

EXPOSE 42000/udp
EXPOSE 8080/tcp

ENTRYPOINT ["/ysf-reflector"]
CMD ["--config", "/etc/ysf-reflector/config.yaml"]
