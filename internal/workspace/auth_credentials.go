package workspace

import "context"

type authCredentials struct {
	Username string
	Password string
}

type authContextKey struct{}

// WithAuthCredentials injects CouchDB admin credentials into the context so the
// auth layer configuration can run without prompting the user.
func WithAuthCredentials(ctx context.Context, username, password string) context.Context {
	return context.WithValue(ctx, authContextKey{}, authCredentials{Username: username, Password: password})
}

func credentialsFromContext(ctx context.Context) (authCredentials, bool) {
	creds, ok := ctx.Value(authContextKey{}).(authCredentials)
	return creds, ok
}
