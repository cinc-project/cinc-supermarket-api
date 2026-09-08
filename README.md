# cinc-supermarket-api

A Go client for the [Chef Supermarket API](https://docs.chef.io/supermarket/supermarket_api/).

## Install

    go get github.com/cinc-project/cinc-supermarket-api

## Usage

Read endpoints are anonymous:

    c, _ := supermarket.NewClient(supermarket.Config{})
    cb, _, _ := c.Cookbooks.Get(context.Background(), "apache2")

Write endpoints (share, delete) use the Chef mixlib-authentication
signed-header protocol (version 1.1, SHA-1 — the version the public
Supermarket accepts). The credentials are the Supermarket username and the RSA
private key registered on that user's Supermarket profile:

    key, _ := supermarket.LoadKeyFile("/home/me/.chef/me.pem")
    c, _ := supermarket.NewClient(supermarket.Config{
        Username: "me",
        Key:      key,
    })
    c.Cookbooks.Share(ctx, "apache2", "Web Servers", tarball)

A `Client` with no credentials refuses write calls with
`ErrUnauthenticatedWrite` without contacting the server.

Version helpers work on the version strings cookbooks return, without a
full semver dependency: `CompareVersions(a, b)` orders two versions,
`LatestVersion(versions)` picks the newest, and `VersionFromURL(u)`
pulls the dotted version out of a `/versions/<v>` URL (Supermarket
encodes the dots as underscores).

## Coverage

| Resource | Endpoint                                              | Method          |
| -------- | ----------------------------------------------------- | --------------- |
| Cookbook | `GET /api/v1/cookbooks`                               | `List`          |
| Cookbook | `GET /api/v1/cookbooks/:name`                         | `Get`           |
| Cookbook | `GET /api/v1/cookbooks/:name/contingent`              | `Contingent`    |
| Cookbook | `GET /api/v1/cookbooks/:name/versions/:v`             | `GetVersion`    |
| Cookbook | `GET /api/v1/cookbooks/:name/versions/:v/download`    | `Download`      |
| Cookbook | `POST /api/v1/cookbooks` (signed, multipart)          | `Share`         |
| Cookbook | `DELETE /api/v1/cookbooks/:name` (signed)             | `Delete`        |
| Cookbook | `DELETE /api/v1/cookbooks/:name/versions/:v` (signed) | `DeleteVersion` |
| Search   | `GET /api/v1/search`                                  | `Cookbooks`     |
| Tools    | `GET /api/v1/tools`                                   | `List`          |
| Tools    | `GET /api/v1/tools/:slug`                             | `Get`           |
| Tools    | `GET /api/v1/tools-search`                            | `Search`        |
| Users    | `GET /api/v1/users/:username`                         | `Get`           |
| Universe | `GET /universe`                                       | `Get`           |
| Universe | `GET /universe` (streaming)                           | `GetStream`     |
| Health   | `GET /api/v1/health`                                  | `Status`        |
| Health   | `GET /api/v1/metrics`                                 | `Metrics`       |

## Errors

Non-2xx responses are wrapped in an `*ErrorResponse` that decodes
Supermarket's `{error_messages, error_code}` envelope. Use
`errors.Is(err, supermarket.ErrNotFound)` (and friends) to check the
class of failure without dealing with the fact that Supermarket
returns 400 with `error_code=NOT_FOUND` rather than 404.

## Testing

`go test ./...` runs the unit suite: it's fast, deterministic, and needs
neither network nor Ruby. It includes:

- **An always-on signing-version guard** (`internal/signing`) asserting we
  sign with a version the public Supermarket verifier accepts (`1.0`/`1.1`).
  This is the cheap insurance against the v1.3-class bug where every signed
  upload 401'd: a 1.3 signature is valid mixlib output, but the server won't
  accept it.
- **A contract replay suite** that decodes recorded production responses under
  `testdata/contract/` through the public types, so a decoder regression
  fails CI deterministically.

Two extra layers reach for outside resources and are gated so the default run
stays fast:

- **Gem-backed verifier test** (`internal/signing`): runs our signed headers
  through mixlib-authentication's real server-side
  `SignatureVerification` (the gold standard) and proves a synthetic v1.3
  request is rejected. It needs Ruby plus the `mixlib-authentication` gem
  (`gem install mixlib-authentication`); without them it skips cleanly rather
  than failing.

- **Live contract suite** — the drift alarm against the real API:

  ```
  go test -tags contract ./...
  ```

  It needs network access to `https://supermarket.chef.io` and hits the
  anonymous read endpoints (list + pagination, search, cookbook show, a
  specific version, `/universe`, tools, and a user record) through the actual
  client. It asserts on shape, types, and required fields — not volatile
  values — and fails (naming the field) if production drifts from what the
  client depends on. It is intentionally excluded from the default suite.

  Coverage boundary: the live suite covers anonymous **reads**; the signed
  **write** path (share/delete) is guarded deterministically by the version
  guard and the mixlib verifier test. A true live signed upload still needs a
  Supermarket account or a local instance — future work.

## License

Licensed under the [Apache License 2.0](LICENSE).
