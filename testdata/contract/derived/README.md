# Derived response bodies

Bodies for responses that cannot be recorded anonymously (the signed write
endpoints and their errors). Each is built from upstream Supermarket's own
views, controllers and locale strings at chef/supermarket `80d517b`, not
written from memory of the API:

| File | Source |
| ---- | ------ |
| `share_created.json` | `app/views/api/v1/cookbook_uploads/create.json.jbuilder` (`json.uri`), status 201 |
| `error_conflict_only_version.json` | `CookbookUploadsController#destroy_version`, status 409, `api.error_messages.only_cookbook_version` |
| `error_authentication_failed.json` | `CookbookUploadsController` `rescue_from Mixlib::Authentication::AuthenticationError`, status 400, `api.error_messages.authentication_request_error` |
| `error_bad_key.json` | `CookbookUploadsController#authenticate_user!`, status 401, `api.error_messages.authentication_key_error` |
| `error_missing_tarball.json` | `rescue_from ActionController::ParameterMissing`, status 400, `api.error_messages.missing_tarball` |
| `error_forbidden_destroy.json` | `CookbookUploadsController#destroy`, status 403, `api.error_messages.unauthorized_destroy_error` |

Re-derive them when moving the pinned upstream commit.
