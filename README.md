# ysf-reflector-go

A [Yaesu System Fusion (YSF)](https://www.yaesu.com/jp/en/wires-x/index.php) UDP reflector written in Go. It links multiple YSF nodes and hotspots together, relaying voice and data frames between all connected clients.

## Features

- Handles the full YSF UDP protocol: `YSFP` (poll/keepalive), `YSFD` (voice/data), `YSFU` (unlink), and `YSFS` (status query)
- Relays incoming frames to all connected nodes except the sender
- Proactively polls clients every 5 seconds to detect drops early
- Evicts idle clients after a configurable timeout
- Transmission watchdog — logs when a transmission starts and ends
- Periodic status dumps every 2 minutes
- Optional per-packet debug logging

## Requirements

- Go 1.21+

## Building

```sh
go build -o ysf-reflector .
```

## Configuration

Copy and edit `config_example.yaml` to `config.yaml`:

```yaml
callsign: K8RON          # Your reflector callsign (max 10 characters)
port: 42000              # UDP port to listen on (standard YSF port is 42000)
timeout: 240             # Seconds before an idle client is disconnected
debug: false             # Log every packet (verbose)

# Reported in YSFS status query responses
id: 0                    # Numeric reflector ID (reported to querying nodes)
name: K8RON Reflector    # Reflector name (max 16 characters)
description: YSF Ref     # Short description (max 14 characters)
```

| Field         | Required | Default | Description                                      |
|---------------|----------|---------|--------------------------------------------------|
| `callsign`    | Yes      | —       | Reflector callsign, max 10 characters            |
| `port`        | No       | `42000` | UDP port to listen on                            |
| `timeout`     | No       | `240`   | Client idle timeout in seconds                   |
| `debug`       | No       | `false` | Log every packet                                 |
| `id`          | No       | `0`     | Numeric ID included in YSFS status responses     |
| `name`        | No       | —       | Reflector name, max 16 characters                |
| `description` | No       | —       | Short description, max 14 characters             |

## Running

```sh
./ysf-reflector -config config.yaml
```

The `-config` flag defaults to `config.yaml` in the current directory.

## Protocol overview

| Magic | Direction         | Description                        |
|-------|-------------------|------------------------------------|
| YSFP  | node ↔ reflector  | Keepalive poll / registration      |
| YSFD  | node → reflector → all others | Voice/data frame relay |
| YSFU  | node → reflector  | Unlink / disconnect request        |
| YSFS  | node → reflector  | Status query; reflector replies with ID, name, description, and node count |

## License

MIT
