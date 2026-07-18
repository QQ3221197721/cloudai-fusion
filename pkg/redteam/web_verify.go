package redteam

import (
	"context"
	"net/http"
)

// web_verify.go completes M3's "reproducible from the signed evidence" promise:
// given a chain and a prior result (whose per-step request hashes are what the
// redteam.web.exploit receipt recorded), re-run the chain and confirm the same
// requests were issued. This is the offline replay a third party performs to
// prove the recorded exploit transcript is faithful.

// ReplayAndVerify re-executes a chain and reports whether every step's request
// fingerprint matches the prior result. Request hashes are deterministic (method
// + path + sorted headers + body), so a faithful chain reproduces them exactly.
func ReplayAndVerify(ctx context.Context, client *http.Client, chain WebChain, prior *WebChainResult) (bool, error) {
	if prior == nil {
		return false, nil
	}
	fresh, err := RunWebChain(ctx, client, chain)
	if err != nil {
		return false, err
	}
	if len(fresh.Steps) != len(prior.Steps) {
		return false, nil
	}
	for i := range fresh.Steps {
		if fresh.Steps[i].RequestHash != prior.Steps[i].RequestHash {
			return false, nil
		}
	}
	return true, nil
}
