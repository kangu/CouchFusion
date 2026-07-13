package workspace

import "context"

type AuthCredentials struct {
	Username string
	Password string
}

type authContextKey struct{}

// WithAuthCredentials injects CouchDB admin credentials into the context so the
// auth layer configuration can run without prompting the user.
func WithAuthCredentials(ctx context.Context, username, password string) context.Context {
	return context.WithValue(ctx, authContextKey{}, AuthCredentials{Username: username, Password: password})
}

func credentialsFromContext(ctx context.Context) (AuthCredentials, bool) {
	creds, ok := ctx.Value(authContextKey{}).(AuthCredentials)
	return creds, ok
}
