[![Go](https://github.com/markmnl/fmsgid/actions/workflows/go.yml/badge.svg)](https://github.com/markmnl/fmsgid/actions/workflows/go.yml)

# fmsgid

fmsgid is an implementation of the [https://github.com/markmnl/fmsg/blob/main/standards/fmsgid.md](fmsg Id standard) written in Go! An fmsg host uses this API to determine if an address exists and lookup associated attributes such as display name and message quotas.

## Goals

fmsgid is designed to be a shim between a real identity management system and an fmsg host. Allowing an fmsg host to lookup details for an fmsg address agnostic of identity provider being used. Then something else needs to sync the between fmsgid and actual identity provider.

## Environment

PostgreSQL environment variables must be set for the database to use, refer to: https://www.postgresql.org/docs/current/libpq-envars.html. 

```
GIN_MODE=release
FMSGID_PORT=8080
```

## Running

```
./fmsgid
```

## Development

To build and run the Go program locally:

### Build

From the `src` directory:

```
go build .
```

This will produce an executable named `fmsgid` (or `fmsgid.exe` on Windows).

