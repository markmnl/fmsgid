[![Build & Test](https://github.com/markmnl/fmsgid/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/markmnl/fmsgid/actions/workflows/go.yml?query=branch%3Amain)
[![Go 1.27+](https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)

# fmsgid

fmsgid is an implementation of the [FMSG-002 Id Standard](https://github.com/markmnl/fmsg/blob/main/standards/fmsg-002-id.md) written in Go! An fmsg host uses this API to determine if an address exists and lookup associated attributes such as display name and message quotas.

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

Requires Go 1.27 or newer.

From the `src` directory:
```
go build .
```

This will produce an executable named `fmsgid` (or `fmsgid.exe` on Windows).

### Test

```
go test ./...
```

Tests that need PostgreSQL are skipped unless `FMSGID_TEST_DATABASE_URL` is set to a connection string, e.g. `postgres://postgres:pass@127.0.0.1:5432/fmsgid_test?sslmode=disable`. They apply `dd.sql` and write and delete rows, so point them at a throwaway database only.

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

## Quota pools

Addresses can share one quota. The nullable `address.quota_pool` column holds the `address_lower` (the case folded address) of the pool owner; `NULL` means the address is its own pool. For example linked addresses such as `@alice_bot@example.com` can share the quota of `@alice@example.com`:

```sql
update address set quota_pool = '@alice@example.com'
where address_lower = '@alice_bot@example.com';
```

- An address in a pool is checked against the pool owner's limits (`limit_*`) and the combined usage of the owner and every address whose `quota_pool` is the owner. Its own limits are kept but not used while it is pooled.
- The pool owner reports the same combined usage, so a pool member's usage counts against the owner too.
- `accepting_new` and `display_name` stay per address.
- Pools are one level deep: a pool owner's own `quota_pool` should be `NULL`. `POST /fmsgid` enforces that; anything writing the column directly should too.
- `quota_pool` references `address (address_lower)`, so the owner must exist first; deleting the owner sets its members' `quota_pool` back to `NULL`.

Set `quota_pool` with `POST /fmsgid`, or have the system syncing your identity provider update the column directly. A role that does so needs `UPDATE (quota_pool)` on `address` besides `SELECT`.

## API Routes

All routes are served over HTTPS under the `/fmsgid` path.

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/fmsgid/:address` | Lookup an fmsg address and return its details including display name, quotas, and usage. The address must be in fmsg format (`@user@example.com`). Returns `AddressDetail` JSON on success, `400` if the address is invalid, `404` if not found. For an address in a [quota pool](#quota-pools) the limits are the pool owner's, the usage is the whole pool's and `quotaPool` is the pool owner's address; `quotaPool` is omitted otherwise. `acceptingNew` is always the address's own. |
| `POST` | `/fmsgid` | Register an address, idempotently. Accepts a JSON body with `address` and optional `display_name`, `accepting_new` (bool) and `quota_pool` (an fmsg address, see [Quota pools](#quota-pools)). A new address takes the `address` table's column defaults in `dd.sql` except for `accepting_new` and `quota_pool` when given. An existing address is left as it is except for `accepting_new` and `quota_pool` when given (`display_name` and quotas are never changed — use CSV sync for those). Setting `accepting_new` `false` stops the address accepting new messages; setting it `true` again reinstates it. `quota_pool` `""` or the address itself takes the address out of any pool. Returns `201` if the address was created, `200` if it already existed, both with `{"created":bool,"reinstated":bool}` where `reinstated` is `true` when the address existed, wasn't accepting new messages and now is. Returns `400` if the body or an address is invalid, `422` with `{"error":"..."}` if `quota_pool` names an address that doesn't exist or is in another pool, or the address owns a pool itself. |
| `POST` | `/fmsgid/send` | Record a send transaction. Accepts an `AddressTx` JSON body with `address`, `ts` (timestamp), and `size`. The address is case folded, and every call is recorded, including several with the same timestamp. Returns `200`, or `400` if the body or address is invalid. |
| `POST` | `/fmsgid/recv` | Record a receive transaction. Accepts an `AddressTx` JSON body with `address`, `ts` (timestamp), and `size`. The address is case folded, and every call is recorded, including several with the same timestamp. Returns `200`, or `400` if the body or address is invalid. |
| `POST` | `/fmsgid/send/release` | Release a sent message's storage, when it is deleted for good. Same body and responses as `/fmsgid/send`. The size comes off `sendSizeTotal` (never below 0) but not off the daily send usage, so deleting messages doesn't raise a daily limit. |
| `POST` | `/fmsgid/recv/release` | Release a received message's storage, when it is deleted for good. Same body and responses as `/fmsgid/recv`. The size comes off `recvSizeTotal` (never below 0) but not off the daily receive usage. |

### systemd

An example systemd service to run fmsgid as a service on startup

ASSUMES:
* Directory `/opt/fmsgid` has been created and contains built executable: `fmsgid`
* Text file `/opt/fmsgid/env` exists containing environment variables (example below)
* User `fmsg` has been created and has
    - read and execute permissions to `/opt/fmsgid/`, e.g. with `chown -R fmsg:fmsg /opt/fmsgid` after `mkdir /opt/fmsgid`
    - write permissions to `/opt/fmsgid` and to the path specified by `FMSGID_CSV_FILE` if it is outside `/opt/fmsgid`

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