# Auditd Alerts (Webhook Receiver + Web UI)

This directory contains a small Go server that receives JSON webhooks and shows them in a simple web UI.

It supports two common inputs:

- Collector/auditd sender format (see `alert_sender`): the UI shows a human title like `nala, acting as root, successfully executed /tmp/x`.
- Generic Splunk-style webhook payloads: stored and displayed as-is.

## Components

- `main.go`: Go HTTP server with endpoints:
  - `POST /webhook` receives alerts (JSON body, or `payload=<json>` form)
  - `GET /` serves the UI
  - `GET /api/alerts` returns stored alerts as JSON
  - `GET /alerts` returns a text view
  - `POST /api/history/reload` reloads `alerts_history.json`
  - `POST /api/history/rotate` rotates history to a new file and clears memory
- `web/`: static UI served by the Go binary (embedded).
- `alert_sender`: tail-based auditd log watcher that sends RED_EXEC alerts to `/webhook`.
- `alerts_history.json`: local on-disk rolling history.
- `website.py`: old/alternate Flask demo webhook viewer (not used by the Go server).

## Quick Start (Go Server)

1. Start the server:

```bash
cd /home/nala/gitHub/Splunk_alerts/audit_server
go run .
```

2. Open the UI:

- `http://<server-ip>:5123/`

3. Send a test webhook:

```bash
curl -sS -X POST "http://<server-ip>:5123/webhook" \
  -H "Content-Type: application/json" \
  -d '{"alert":"RED_EXEC","host":"debian","exe":"/tmp/x","comm":"x","uid":"0","euid":"0","auid":"1000","pid":"123","ppid":"1","tty":"pts0","key":"red_exec","audit":"1771200020.462:7531","text":"demo","raw":"type=SYSCALL msg=audit(1771200020.462:7531): ... success=yes ... AUID=\"nala\""}'
```

### Port

Default port is `5123`. Override with:

```bash
PORT=8080 go run .
```

## Using `alert_sender` (Auditd -> Webhook)

`alert_sender` watches `/var/log/audit/audit.log` for successful `execve` syscalls run as root and sends a JSON alert to the Go server.

### 1) Set the receiver IP

Edit the `WEBHOOK=` line in `alert_sender` to point to your Go server:

```bash
WEBHOOK="http://10.0.4.220:5123/webhook"
```

Change `10.0.4.220` to the IP of the machine running `go run .`.

### 2) Adjust what is considered "red"

In `alert_sender`, these arrays control what triggers an alert:

- `ALLOW_PREFIX`: normal/allowed executable paths
- `BAD_PREFIX`: suspicious paths (examples: `/tmp/`, `/dev/shm/`, `/var/tmp/`, etc.)

An alert is sent when:

- the event is `SYSCALL` + `syscall=59` (execve) + `success=yes`, and
- `uid==0` or `euid==0`, and
- the `exe` is in a bad prefix OR not in an allowed prefix

### 3) Run it

This script needs access to `/var/log/audit/audit.log` (usually root) and requires `python3` for safe JSON encoding.

```bash
sudo bash ./alert_sender
```

## Notes / Troubleshooting

- If you see alerts show up as `unparsed`, it means the receiver could not parse the incoming body as JSON. With the current `alert_sender` this should not happen (it uses Python JSON encoding to escape control characters found in audit logs).
- The UI uses the `title` field when present; otherwise it falls back to `host`.

