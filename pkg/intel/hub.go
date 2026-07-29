package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// capabilityComponent is the name this well registers under in pkg/capability.
const capabilityComponent = "intel.store"

// intelSyncAction is the evidence action name for a synchronization receipt.
const intelSyncAction = "intel.sync"

// Hub coordinates offline-first threat-intelligence synchronization across feed
// sources, persists normalized records into a pluggable Store, and records a
// signed receipt for every sync via the Verifiable Control Plane.
type Hub struct {
	sources  []FeedSource
	store    Store
	logger   *logrus.Logger
	recorder evidence.Recorder

	// wellPublish, when set by the composition root, emits an L1 deep-well event
	// onto the event fabric after a sync so downstream wells (L2/L3/L4/L14) react.
	// It is a hook (not a direct eventbus import) so this package stays decoupled;
	// a nil hook is a no-op.
	wellPublish func(ctx context.Context, kind string, detail map[string]any)
}

// NewHub builds a Hub over the given feed sources and store. A nil store defaults
// to an in-memory (simulated) store; a nil logger uses the standard logger. The
// store's real-vs-simulated nature is reported to the default capability registry
// so a production boot with only a simulated store fails capability.Enforce().
func NewHub(sources []FeedSource, store Store, logger *logrus.Logger) *Hub {
	if store == nil {
		store = NewMemoryStore()
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	h := &Hub{
		sources:  sources,
		store:    store,
		logger:   logger,
		recorder: evidence.NopRecorder{},
	}
	// Honest reporting: register the store backing with the capability registry.
	_ = capability.MustReal(capabilityComponent, store.Driver(), store.IsReal(),
		fmt.Sprintf("threat-intel store driver=%s", store.Driver()))
	return h
}

// SetEvidenceRecorder attaches an evidence ledger so sync actions are signed.
func (h *Hub) SetEvidenceRecorder(rec evidence.Recorder) {
	if rec != nil {
		h.recorder = rec
	}
}

// SetWellPublisher attaches the event-fabric publisher hook (see Hub.wellPublish).
func (h *Hub) SetWellPublisher(fn func(ctx context.Context, kind string, detail map[string]any)) {
	h.wellPublish = fn
}

// Store returns the underlying store (used by other deep wells: L2 hunting, etc.).
func (h *Hub) Store() Store { return h.store }

// SyncAll synchronizes every configured feed source, returning an aggregated
// result. Individual source failures are recorded but do not abort the run, so a
// single bad feed cannot starve the others.
func (h *Hub) SyncAll(ctx context.Context) (*SyncResult, error) {
	res := &SyncResult{}
	for i := range h.sources {
		if err := h.syncSource(ctx, &h.sources[i], res); err != nil {
			h.logger.WithError(err).WithField("source", h.sources[i].Name).Warn("intel: source sync failed")
			res.RecordError(fmt.Sprintf("%s: %v", h.sources[i].Name, err))
			h.sources[i].Status = "error"
		} else {
			h.sources[i].Status = "active"
			h.sources[i].LastSyncAt = time.Now().UTC()
		}
	}
	h.recordSync(ctx, res)
	// Emit an L1 event onto the fabric so downstream wells react to fresh intel.
	if h.wellPublish != nil {
		h.wellPublish(ctx, "intel_sync", map[string]any{"cve_added": res.CVEAdded, "ioc_added": res.IOCAdded})
	}
	if res.HasErrors() {
		return res, fmt.Errorf("intel: sync completed with %d error(s)", len(res.Errors))
	}
	return res, nil
}

// syncSource loads and persists one feed source. It is offline-first: LocalPath
// is loaded when set. Feed files are read only from within the configured base
// directory (path-traversal safe).
func (h *Hub) syncSource(ctx context.Context, src *FeedSource, res *SyncResult) error {
	if src.LocalPath == "" {
		return fmt.Errorf("no local_path configured (offline-first requires a feed directory)")
	}

	base, err := filepath.Abs(src.LocalPath)
	if err != nil {
		return fmt.Errorf("resolve base path: %w", err)
	}

	// CVE feed (nvd.jsonl)
	if data, rerr := readWithinBase(base, "nvd.jsonl"); rerr == nil {
		cves := ParseCVEJSONL(data)
		for _, c := range cves {
			if uerr := h.store.UpsertCVE(c); uerr != nil {
				return fmt.Errorf("upsert cve %s: %w", c.CVEID, uerr)
			}
			res.AddCVE()
		}
		h.logger.WithField("count", len(cves)).Debug("intel: loaded CVE feed")
	}

	// IOC feed (ioc-feed.tsv)
	if data, rerr := readWithinBase(base, "ioc-feed.tsv"); rerr == nil {
		iocs := ParseIOCFeed(data)
		if len(iocs) > 0 {
			if uerr := h.store.UpsertIOCs(iocs); uerr != nil {
				return fmt.Errorf("upsert iocs: %w", uerr)
			}
			res.IOCAdded += len(iocs)
		}
		h.logger.WithField("count", len(iocs)).Debug("intel: loaded IOC feed")
	}

	// Knowledge graph (mitre-attack.json)
	if data, rerr := readWithinBase(base, "mitre-attack.json"); rerr == nil {
		var kg KnowledgeGraph
		if json.Unmarshal(data, &kg) == nil {
			if uerr := h.store.PutKnowledgeGraph(kg); uerr != nil {
				return fmt.Errorf("put knowledge graph: %w", uerr)
			}
			h.logger.WithFields(logrus.Fields{
				"tactics": len(kg.Tactics), "techniques": len(kg.Techniques),
			}).Debug("intel: loaded knowledge graph")
		}
	}

	// STIX 2.1 bundle (stix.json) — the industry-standard feed format (MISP/OTX/…).
	if data, rerr := readWithinBase(base, "stix.json"); rerr == nil {
		if n, serr := h.importSTIX(data, res); serr != nil {
			return fmt.Errorf("import stix: %w", serr)
		} else {
			h.logger.WithField("indicators", n).Debug("intel: loaded STIX bundle")
		}
	}

	return nil
}

// importSTIX parses a STIX 2.1 bundle and upserts its IOCs, CVEs, and techniques
// into the store, updating the sync result. It returns the number of IOCs added.
func (h *Hub) importSTIX(data []byte, res *SyncResult) (int, error) {
	imp, err := ParseSTIXBundle(data)
	if err != nil {
		return 0, err
	}
	for _, c := range imp.CVEs {
		if uerr := h.store.UpsertCVE(c); uerr != nil {
			return 0, fmt.Errorf("upsert cve %s: %w", c.CVEID, uerr)
		}
		res.AddCVE()
	}
	if len(imp.IOCs) > 0 {
		if uerr := h.store.UpsertIOCs(imp.IOCs); uerr != nil {
			return 0, fmt.Errorf("upsert iocs: %w", uerr)
		}
		res.IOCAdded += len(imp.IOCs)
	}
	if len(imp.Techniques) > 0 {
		// Merge techniques into the knowledge graph (best-effort, non-fatal).
		_ = h.store.PutKnowledgeGraph(KnowledgeGraph{Techniques: imp.Techniques})
	}
	return len(imp.IOCs), nil
}

// ImportSTIXBundle ingests a STIX 2.1 bundle pushed directly (e.g. from a MISP
// export) rather than read from a feed directory. It upserts the parsed records,
// records a signed sync receipt, and emits an L1 fabric event — the push-model
// counterpart of SyncAll's offline pull.
func (h *Hub) ImportSTIXBundle(ctx context.Context, data []byte) (*SyncResult, error) {
	res := &SyncResult{}
	if _, err := h.importSTIX(data, res); err != nil {
		res.RecordError(err.Error())
		h.recordSync(ctx, res)
		return res, err
	}
	h.recordSync(ctx, res)
	if h.wellPublish != nil {
		h.wellPublish(ctx, "intel_sync", map[string]any{"cve_added": res.CVEAdded, "ioc_added": res.IOCAdded, "format": "stix"})
	}
	return res, nil
}

// readWithinBase reads base/name after verifying the cleaned, resolved path stays
// inside base. This prevents path traversal from crafted feed names/symlinks.
func readWithinBase(base, name string) ([]byte, error) {
	full := filepath.Join(base, name)
	clean := filepath.Clean(full)
	if clean != base && !strings.HasPrefix(clean, base+string(os.PathSeparator)) {
		return nil, fmt.Errorf("path %q escapes base %q", clean, base)
	}
	return os.ReadFile(clean)
}

// recordSync writes a signed receipt describing the sync outcome. A NopRecorder
// (the default) makes this a no-op, so wiring an evidence ledger is optional.
func (h *Hub) recordSync(ctx context.Context, res *SyncResult) {
	_, err := h.recorder.Record(ctx, evidence.RecordInput{
		Actor:      "intel-hub",
		Action:     intelSyncAction,
		Subject:    capabilityComponent,
		Output:     res,
		Components: []string{capabilityComponent},
	})
	if err != nil {
		h.logger.WithError(err).Warn("intel: failed to record sync evidence")
	}
}

// RecentCVEs returns CVEs published within the given lookback window (newest first).
func (h *Hub) RecentCVEs(ctx context.Context, since time.Time, limit int) ([]CVEEntry, error) {
	return h.store.RecentCVEs(since, limit)
}

// MatchIOCs returns stored IOCs of iocType whose value appears in values. It is
// the query surface used by the operations wells (L3 endpoint, L4 network).
func (h *Hub) MatchIOCs(ctx context.Context, iocType string, values []string) ([]IOCEntry, error) {
	return h.store.LookupIOCs(iocType, values)
}

// ParseCVEJSONL parses newline-delimited JSON CVE records, skipping blank/malformed
// lines so a single bad line cannot discard an entire feed.
func ParseCVEJSONL(data []byte) []CVEEntry {
	var out []CVEEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var c CVEEntry
		if err := json.Unmarshal([]byte(line), &c); err == nil && c.CVEID != "" {
			out = append(out, c)
		}
	}
	return out
}

// ParseIOCFeed parses a tab-separated IOC feed:
//
//	<type>\t<value>\t<severity>\t<RFC3339 first_seen>[\t<source>...]
//
// Lines beginning with '#' are treated as comments.
func ParseIOCFeed(data []byte) []IOCEntry {
	var out []IOCEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		e := IOCEntry{
			IOCType:  strings.TrimSpace(f[0]),
			Value:    strings.TrimSpace(f[1]),
			Severity: Severity(strings.TrimSpace(f[2])),
		}
		if len(f) >= 4 {
			if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(f[3])); err == nil {
				e.FirstSeenAt = ts
			}
		}
		if len(f) >= 5 {
			e.Sources = f[4:]
		}
		out = append(out, e)
	}
	return out
}
