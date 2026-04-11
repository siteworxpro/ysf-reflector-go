# ysf-reflector-go

A [Yaesu System Fusion (YSF)](https://www.yaesu.com/jp/en/wires-x/index.php) UDP reflector written in Go. It links
multiple YSF nodes and hotspots together, relaying voice and data frames between all connected clients.

## Features

- Handles the full YSF UDP protocol: `YSFP` (poll/keepalive), `YSFD` (voice/data), `YSFU` (unlink), and `YSFS` (status
  query)
- Relays incoming frames to all connected nodes except the sender
- Proactively polls clients every 5 seconds to detect drops early
- Evicts idle clients after a configurable timeout
- Transmission watchdog — logs when a transmission starts and ends
- Periodic status dumps every 2 minutes
- Optional per-packet debug logging
- Parrot mode — buffers each transmission and replays it to all connected nodes after TX ends
- HTTP dashboard showing connected nodes in real time
- `/api/clients` JSON endpoint for programmatic access

## Requirements

- Go 1.21+

## Docker

Pre-built images are published to Docker Hub at [
`siteworxpro/ysf-reflector-go`](https://hub.docker.com/r/siteworxpro/ysf-reflector-go).

### Quick start

Create a `config.yaml` (see [Configuration](#configuration) below), then run:

```sh
docker run -d \
  --name ysf-reflector \
  -v /path/to/config.yaml:/etc/ysf-reflector/config.yaml \
  -p 42000:42000/udp \
  -p 8080:8080 \
  siteworxpro/ysf-reflector-go
```

### Docker Compose

```yaml
services:
  ysf-reflector:
    image: siteworxpro/ysf-reflector-go:latest
    restart: unless-stopped
    volumes:
      - ./config.yaml:/etc/ysf-reflector/config.yaml
    ports:
      - "42000:42000/udp"
      - "8080:8080"
```

### Building the image locally

```sh
docker build -t ysf-reflector .

# With a version label
docker build --build-arg VERSION=1.2.3 -t ysf-reflector:1.2.3 .
```

The config file is expected at `/etc/ysf-reflector/config.yaml` inside the container.

## Building

```sh
go build -o ysf-reflector .
```

## Configuration

Copy and edit `config_example.yaml` to `config.yaml`:

```yaml
callsign: K8RON          # Your reflector callsign (max 10 characters)
port: 42000              # UDP port to listen on (standard YSF port is 42000)
http_port: 8080          # HTTP dashboard port
timeout: 240             # Seconds before an idle client is disconnected
debug: false             # Log every packet (verbose)
parrot: false            # Buffer each transmission and replay it to all nodes after TX ends

# Reported in YSFS status query responses
id: 0                    # Numeric reflector ID (reported to querying nodes)
name: K8RON Reflector    # Reflector name (max 16 characters)
description: YSF Ref     # Short description (max 14 characters)
```

| Field         | Required | Default | Description                                     |
|---------------|----------|---------|-------------------------------------------------|
| `callsign`    | Yes      | —       | Reflector callsign, max 10 characters           |
| `port`        | No       | `42000` | UDP port to listen on                           |
| `http_port`   | No       | `8080`  | HTTP dashboard port                             |
| `timeout`     | No       | `240`   | Client idle timeout in seconds                  |
| `debug`       | No       | `false` | Log every packet                                |
| `parrot`      | No       | `false` | Buffer TX and replay to all nodes after TX ends |
| `id`          | No       | `0`     | Numeric ID included in YSFS status responses    |
| `name`        | No       | —       | Reflector name, max 16 characters               |
| `description` | No       | —       | Short description, max 14 characters            |

## Running

```sh
./ysf-reflector -config config.yaml
```

The `-config` flag defaults to `config.yaml` in the current directory.

## Web dashboard

When the reflector is running, a live dashboard is available at `http://localhost:8080` (or whatever `http_port` is set
to). It lists all currently connected nodes with their callsign, IP address, and time since last heard.

A JSON API is also available for programmatic access:

```
GET /api/clients
```

```json
[
  {
    "callsign": "K8RON",
    "addr": "1.2.3.4:42000",
    "last_seen": "2026-04-10T17:00:00Z"
  }
]
```

## Protocol overview

| Magic | Direction                     | Description                                                                |
|-------|-------------------------------|----------------------------------------------------------------------------|
| YSFP  | node ↔ reflector              | Keepalive poll / registration                                              |
| YSFD  | node → reflector → all others | Voice/data frame relay                                                     |
| YSFU  | node → reflector              | Unlink / disconnect request                                                |
| YSFS  | node → reflector              | Status query; reflector replies with ID, name, description, and node count |

## License

Copyright 2026 Siteworxpro LLC

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the “Software”), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED “AS IS”, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
