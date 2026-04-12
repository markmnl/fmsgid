# Agent Instructions

## README

Keep `README.md` up to date when making changes. In particular:

- Update the **API Routes** section when routes are added, removed, or changed.
- Update the **Environment** section when environment variables are added or changed.
- Update the **Build** or **Running** sections if build steps or runtime requirements change.

## Code

- This is a Go project using the Gin web framework and PostgreSQL (via pgx).
- Source code is in the `src/` directory.
- fmsg addresses are case-insensitive and must be normalised using Unicode case folding (`cases.Fold()` from `golang.org/x/text/cases`), not `strings.ToLower()`.
