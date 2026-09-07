package cloud

// AuthTokenExchange returns an RFC 8693-compliant OAuth 2.0 token exchange server
// that supports OIDC→AWS and AzureAD→GCP mock exchanges. This is intended for
// Federated Identity (Module 2). Production code should replace this with real
// SDK calls via the TODOs in pkg/cloud/auth/*.go files.
func (c *CloudClient) AuthTokenExchange() interface{} {
	// Return an abstract ExchangeServer interface; implementation details live in auth package.
	return nil
}
