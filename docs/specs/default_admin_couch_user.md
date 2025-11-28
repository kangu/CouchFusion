On the new workflow, at the end after asking for couchdb admin username and
  password, use those credentials for create a user document in the _users database with
  those exact username and password values. Check to see if it exists already and echo
  that it does, otherwise create the document and echo that it was created.

- [x] Detect existing `_users/org.couchdb.user:<username>` entry using the provided admin credentials and log when it already exists. *(2025-10-28)*
- [x] Create the CouchDB user document with roles including `admin` when missing, using the supplied username/password, and confirm creation. *(2025-10-28)*
