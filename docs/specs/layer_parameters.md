Each option for a layer can implement custom logic asking for arguments and performing
certain operations with them.

The auth layer for example, when selected inside create_app or when added with add_layer (not yet implemented), should ask for the following parameters:
- CouchDB admin username
- CouchDB admin password

Combine the username and password together with btoa() for a valid Basic Auth header. Persist the
value as for COUCHDB_ADMIN_AUTH in the .env file of the project root.
Run a query on the CouchDB config endpoint to retrieve the COUCHDB_COOKIE_SECRET env variable that
is to also be persisted in the same .env file. The value comes from the config chttpd_auth\secret.
Confirm at the end that the two values have been written to the .env file.