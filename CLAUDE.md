# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Project

A Go client library for the [Chef Supermarket API](https://docs.chef.io/supermarket/supermarket_api/).
It is consumed by `cinc-cli` and others, so exported types and method
signatures are a public API — avoid breaking them without reason. Prefer
additive fixes (new `Option`, new sentinel error, stricter validation that
turns a silent wrong answer into an error) over signature changes.

Module path is `github.com/cinc-project/cinc-supermarket-api`. It has been
renamed twice; don't reintroduce `tas50/…` or `cinc-supermarket` paths.

## Build & test

- There is no Makefile. Build with `go build ./...` and test with
  `go test ./...`. Go 1.26.
- The default suite makes no network calls — every test stands up an
  `httptest.Server` — and takes roughly 2–3 seconds. Most of that is RSA
  key generation and the Ruby-backed signing tests, not I/O. Run the
  whole thing on every change; don't reach for a narrower scope.
- Single test: `go test -run TestName ./...`.
- Run `gofmt -w .` and `go vet ./...` before committing; CI gates on both.
- Coverage: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`.
  It sits around 94%.

### Test layers

Four, in increasing cost. The first two always run; the last two need
outside resources and degrade gracefully.

1. **Unit tests** — `httptest`-based, one `<name>_test.go` per source file.
2. **Contract replay** (`contract_replay_test.go`) — decodes recorded
   production bodies from `testdata/contract/` through the public types, so
   a decoder regression fails CI deterministically.
3. **Gem-backed verifier** (`internal/signing`) — runs our signed headers
   through mixlib-authentication's real server-side `SignatureVerification`.
   Needs Ruby plus `gem install mixlib-authentication`; skips cleanly when
   absent. This is the gold-standard check on the signing implementation.
4. **Live contract suite** (`contract_live_test.go`) — behind the `contract`
   build tag, so it is excluded from the default run:
   `go test -tags contract ./...`. Hits the real
   `https://supermarket.chef.io` anonymous read endpoints and fails, naming
   the field, if production drifts from what the client depends on. Note
   that `go vet ./...` does not check tagged files; use
   `go vet -tags contract ./...` after touching it.

## Layout

- `client.go` — the `Client`, which wires one service per resource:
  `Cookbooks`, `Search`, `Tools`, `Users`, `Universe`, `Health`. Each
  service lives in its own `<name>.go` with a sibling `<name>_test.go`.
- `transport.go`, `options.go`, `errors.go`, `pagination.go`,
  `response.go`, `version.go`, `util.go` — shared plumbing: functional
  options (`WithHTTPClient`, `WithSkipTLSVerify`, …), typed errors
  (e.g. `ErrNotFound`), pagination, and the version-comparison helpers.
- `internal/signing` — the Chef mixlib-authentication signed-header
  protocol used by the write endpoints (share/delete). Read endpoints are
  anonymous.
- `testdata/test_key.pem` — RSA key fixture, used only by the
  `internal/signing` tests. The root-package tests generate a fresh
  2048-bit key via `testRSAKey`.
- `testdata/contract/` — recorded production response bodies for the
  replay suite.

## Domain gotchas

These have each caused a real bug. Check them before "modernizing" anything.

- **Sign version is 1.1 (SHA-1), not 1.3 (SHA-256).** The public
  Supermarket's verifier only accepts 1.0/1.1; a 1.3 signature is valid
  mixlib output that the server rejects, and every signed upload 401s.
  There is an always-on guard test asserting this. Don't "upgrade" it
  without a Supermarket that accepts 1.3.
- **The share upload hashes the tarball alone.** For multipart file
  uploads, `X-Ops-Content-Hash` covers only the uploaded file's bytes, not
  the whole multipart envelope. That is what `request.signBody` exists for.
- **Supermarket returns 400 with `error_code=NOT_FOUND`, not 404.** Match
  on the sentinels with `errors.Is(err, ErrNotFound)`, never on the status
  code.
- **A `Page` with zero items is terminal** even when `Total` claims more.
  `HasMore` enforces this; without it a pagination loop spins forever.
- **`http.Client.Timeout` is a whole-transaction deadline.** The timer keeps
  running after `Do` returns and interrupts reading of `Response.Body`, so it
  is the wrong bound for `Universe.GetStream` and `Cookbooks.Download`, whose
  payloads are large and read incrementally. Bound those with `ctx` and use
  transport-level phase timeouts, never a total deadline.

## Conventions

- **TDD.** Write a failing test first, watch it fail for the right reason,
  then the minimal code to pass. If a new test passes immediately, suspect
  the test before believing the code.
- **Watch out for assertions a Go `httptest` handler cannot make.** The
  server canonicalizes every incoming header key, so a handler-side check
  can never observe the casing a client actually sent — such a test passes
  against deliberately broken client code. Assert on the client-side
  request (via a capturing `RoundTripper`) when header casing matters.
- Tests build a client through the `newTestClient` helper in
  `testhelpers_test.go` (`signed=true` attaches credentials for write
  endpoints). Use it rather than hand-rolling a `Client`; use `testRSAKey(t)`
  for keys.
- Assert on behaviour a caller could observe, and say in the failure
  message what the wrong value implies, not just that it differed.
