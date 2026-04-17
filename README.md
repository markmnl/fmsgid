[![Go](https://github.com/markmnl/fmsgid/actions/workflows/go.yml/badge.svg)](https://github.com/markmnl/fmsgid/actions/workflows/go.yml)

# fmsgid

fmsgid is an implementation of the [https://github.com/markmnl/fmsg/blob/main/standards/fmsgid.md](fmsg Id standard) written in Go! An fmsg host uses this API to determine if an address exists and lookup associated attributes such as display name and message quotas.

## Goals

fmsgid is designed to be a shim between a real identity management system and an fmsg host. Allowing an fmsg host to lookup details for an fmsg address agnostic of identity provider being used. Then something else needs to sync between fmsgid and actual identity provider.

## Environment

See .env.example for a list of environment variables which can be copied to a `.env` file and replaced by actual values to use. PostgreSQL environment variables must be set for the database to use, refer to: https://www.postgresql.org/docs/current/libpq-envars.html. 

```
GIN_MODE=release
FMSGID_PORT=8080
FMSGID_CSV_FILE=/path/to/addresses.csv
```

| Variable | Description | Default |
|----------|-------------|---------|
| `GIN_MODE` | Gin framework mode (`release`, `debug`, `test`) | `debug` |
| `FMSGID_PORT` | Port to listen on | `8080` |
| `FMSGID_CSV_FILE` | Path to a CSV file to sync addresses from. When set, fmsgid watches the file for changes and automatically syncs the `address` table. When unset, CSV sync is disabled. | _(unset)_ |

## Build

From the `src` directory:
```
go build .
```

This will produce an executable named `fmsgid` (or `fmsgid.exe` on Windows).

## Running

PostgreSQL database with tables created from `dd.sql` is required. The database connection details are got from the environment and the user must have write access to these tables. Run the executable which will update environment with any variables in `.env` file alongside if present.

```
./fmsgid
```

## CSV Identity Provider

When `FMSGID_CSV_FILE` is set, fmsgid reads the CSV file at startup and watches it for changes using filesystem notifications. On each change the `address` table is synced:

- Addresses in the CSV are **upserted** (created or updated).
- Addresses in the database but **not** in the CSV have `accepting_new` set to `false` (they are not deleted).

The CSV must have a header row. Column names correspond to the `address` table columns. Only the `address` column is required; all others are optional and use the same defaults as the table.

To get started, copy the example file and edit it with your addresses:

```
cp addresses.csv.example addresses.csv
```

Then set `FMSGID_CSV_FILE=addresses.csv` in your `.env` file (or environment).

Available columns:

| Column | Required | Default | Description |
|--------|----------|---------|-------------|
| `address` | yes | | fmsg address (e.g. `@alice@example.com`) |
| `display_name` | no | _(empty)_ | Display name |
| `accepting_new` | no | `true` | Whether the address accepts new messages |
| `limit_recv_size_total` | no | `102400000` | Total received size limit (bytes) |
| `limit_recv_size_per_msg` | no | `10240` | Max size per received message |
| `limit_recv_size_per_1d` | no | `102400` | Received size limit per day |
| `limit_recv_count_per_1d` | no | `1000` | Received message count limit per day |
| `limit_send_size_total` | no | `102400000` | Total sent size limit |
| `limit_send_size_per_msg` | no | `10240` | Max size per sent message |
| `limit_send_size_per_1d` | no | `102400` | Sent size limit per day |
| `limit_send_count_per_1d` | no | `1000` | Sent message count limit per day |

See `addresses.csv.example` for a complete example with all columns.

## API Routes

All routes are served over HTTPS under the `/fmsgid` path.

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/fmsgid/:address` | Lookup an fmsg address and return its details including display name, quotas, and usage. The address must be in fmsg format (`@user@example.com`). Returns `AddressDetail` JSON on success, `400` if the address is invalid, `404` if not found. |
| `POST` | `/fmsgid/send` | Record a send transaction. Accepts an `AddressTx` JSON body with `address`, `ts` (timestamp), and `size`. |
| `POST` | `/fmsgid/recv` | Record a receive transaction. Accepts an `AddressTx` JSON body with `address`, `ts` (timestamp), and `size`. |

### systemd

An example systemd service to run fmsgid as a service on startup

ASSUMES: 
* Directory `/opt/fmsgid` has been created and contains built executable: `fmsgid`
* Text file `/opt/fmsgid/env` exists containing environment variables (example below)
* User `fmsg` has been created and has
    - read and execute permissions to `/opt/fmsgid/`, e.g. with `chown -R fmsg:fmsg /opt/fmsgid` after `mkdir /opt/fmsgid`
    - write permissions to FMSG_DATA_DIR

`/etc/systemd/system/fmsgid.service`

```
[Unit]
Description=fmsgid HTTP API
After=network-online.target
Wants=network-online.target

[Service]
Type=simple

User=fmsg
Group=fmsg

EnvironmentFile=/opt/fmsgid/env

ExecStart=/opt/fmsgid/fmsgid
WorkingDirectory=/opt/fmsgid

Restart=on-failure
RestartSec=3

# --- Filesystem access ---
ReadWritePaths=/opt/fmsgid
PrivateTmp=true

# --- Hardening ---
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true

# --- Logging ---
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

### env

```
GIN_MODE=release
FMSGID_PORT=8080
FMSGID_CSV_FILE=addresses.csv

PGHOST=127.0.0.1
PGPORT=5432
PGUSER=
PGPASSWORD=
PGDATABASE=fmsgid
```

```
sudo systemctl daemon-reload
sudo systemctl enable fmsgid
sudo systemctl start fmsgid
```