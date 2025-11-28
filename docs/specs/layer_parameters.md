Each option for a layer can implement custom logic asking for arguments and performing
certain operations with them.

- [x] Auth layer prompts for CouchDB admin username when selected within `new`. *(2025-10-28)*
- [x] Auth layer prompts for CouchDB admin password when selected within `new`. *(2025-10-28)*
- [x] Combine the username and password with base64 to produce a Basic Auth header and persist it as `COUCHDB_ADMIN_AUTH` in the project `.env`. *(2025-10-28)*
- [x] Fetch CouchDB `chttpd_auth/secret` and persist it as `COUCHDB_COOKIE_SECRET` in the project `.env`. *(2025-10-28)*
- [x] Confirm both values are written to the `.env` file after configuration completes. *(2025-10-28)*

The same parameter handling should be re-used when the forthcoming `add_layer` workflow is implemented.
