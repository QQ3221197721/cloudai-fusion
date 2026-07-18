package evidence

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics.go exposes the evidence plane over Prometheus so operators can see the
// log growing and, crucially, alert if verification ever fails (a verify failure
// means tamper or key mismatch). Uses promauto on the default registry, matching
// the rest of pkg/metrics.
var (
	receiptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudai_evidence_receipts_total",
		Help: "Total evidence receipts recorded, labeled by action.",
	}, []string{"action"})

	verifyTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudai_evidence_verify_total",
		Help: "Total evidence chain verifications, labeled by result (valid|invalid).",
	}, []string{"result"})

	ledgerSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cloudai_evidence_ledger_size",
		Help: "Current number of records in the evidence ledger (max observed Seq).",
	})
)

// observeRecord updates metrics after a receipt is durably appended.
func observeRecord(e *Evidence) {
	if e == nil {
		return
	}
	receiptsTotal.WithLabelValues(e.Action).Inc()
	ledgerSize.Set(float64(e.Seq))
}

// ObserveVerify records the outcome of a chain verification. Handlers/CLIs that
// verify the ledger should call this so a spike in "invalid" is alertable.
func ObserveVerify(valid bool) {
	result := "invalid"
	if valid {
		result = "valid"
	}
	verifyTotal.WithLabelValues(result).Inc()
}
