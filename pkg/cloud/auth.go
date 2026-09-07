package cloud

// exchangeServer is a minimal placeholder for RFC 8693 OAuth 2.0 token exchange.
// The real implementation lives in pkg/cloud/auth; this file just provides a stub API.
// Production code will use RealExchangeServer from auth package directly.
type ExchangeServer struct{}

// NewExchangeServer creates a new exchange server instance.
func NewExchangeServer() *ExchangeServer {
	return &ExchangeServer{}
}
