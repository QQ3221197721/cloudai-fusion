// Package gitops — lightweight ArgoCD REST client.
//
// Rather than pulling ArgoCD's heavy official SDK, this drives ArgoCD via its
// stable REST API (POST /api/v1/applications/{name}/sync) using only net/http.
// It is configured from the environment (ARGOCD_SERVER + ARGOCD_AUTH_TOKEN);
// when unset, SyncApplication records a simulated sync (rejected under
// run_mode=production). This turns the previously-simulated GitOps sync into a
// real integration when an ArgoCD endpoint is provided.
package gitops

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type argoCDClient struct {
	server string
	token  string
	http   *http.Client
}

// newArgoCDClientFromEnv builds an ArgoCD client from ARGOCD_SERVER and
// ARGOCD_AUTH_TOKEN. Returns (nil, false) when not configured.
func newArgoCDClientFromEnv() (*argoCDClient, bool) {
	server := strings.TrimRight(os.Getenv("ARGOCD_SERVER"), "/")
	token := os.Getenv("ARGOCD_AUTH_TOKEN")
	if server == "" || token == "" {
		return nil, false
	}
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		server = "https://" + server
	}
	return &argoCDClient{
		server: server,
		token:  token,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				// ArgoCD servers commonly use self-signed certs; allow opt-in skip.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: os.Getenv("ARGOCD_INSECURE") == "true"}, //nolint:gosec // opt-in via env for self-signed ArgoCD
			},
		},
	}, true
}

// Sync triggers a real ArgoCD application sync for the given revision.
func (c *argoCDClient) Sync(ctx context.Context, appName, revision string) error {
	payload := map[string]interface{}{
		"revision": revision,
		"prune":    true,
		"dryRun":   false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/api/v1/applications/%s/sync", c.server, appName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusMultipleChoices {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("argocd sync %q returned HTTP %d: %s", appName, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
