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
```

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

## API Routes

All routes are served over HTTPS under the `/fmsgid` path.

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/fmsgid/:address` | Lookup an fmsg address and return its details including display name, quotas, and usage. The address must be in fmsg format (`@user@example.com`). Returns `AddressDetail` JSON on success, `400` if the address is invalid, `404` if not found. |
| `POST` | `/fmsgid/send` | Record a send transaction. Accepts an `AddressTx` JSON body with `address`, `ts` (timestamp), and `size`. |
| `POST` | `/fmsgid/recv` | Record a receive transaction. Accepts an `AddressTx` JSON body with `address`, `ts` (timestamp), and `size`. |
