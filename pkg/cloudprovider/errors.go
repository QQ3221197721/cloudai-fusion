package cloudprovider

import "errors"

var (
	// ErrCredentialsRequired is returned by a cloud adapter operating in
	// offline mode. It signals that the operation cannot proceed until valid
	// credentials are configured for the provider. This is the honest failure
	// mode: the adapter refuses rather than faking a successful result.
	ErrCredentialsRequired = errors.New("cloudprovider: cloud credentials required (running in offline credentials-required mode)")

	// ErrLiveBackendUnavailable is returned when credentials are present but
	// the live cloud SDK backend is not linked into this build. It keeps the
	// adapter honest: having credentials does not imply a working transport.
	ErrLiveBackendUnavailable = errors.New("cloudprovider: credentials present but live cloud SDK backend is not linked in this build")

	// ErrInstanceNotFound is returned when an instance ID does not exist.
	ErrInstanceNotFound = errors.New("cloudprovider: instance not found")

	// ErrInvalidRequest is returned when a create request is malformed
	// (e.g. missing instance Type).
	ErrInvalidRequest = errors.New("cloudprovider: invalid instance request")

	// ErrUnknownInstanceType is returned by GetPricing when the requested
	// instance type is absent from the price catalog.
	ErrUnknownInstanceType = errors.New("cloudprovider: unknown instance type in pricing catalog")

	// ErrProviderNotRegistered is returned by the Registry when no provider is
	// registered under the requested kind.
	ErrProviderNotRegistered = errors.New("cloudprovider: provider not registered")
)
