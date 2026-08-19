// types.ts — TypeScript mirrors of the CloudAI Fusion backend contracts.
// These shapes intentionally track the Go source of truth so the console never
// invents fields the API does not actually return:
//   - Capabilities: pkg/api/router.go -> handleCapabilities()
//                   pkg/capability/registry.go -> Backend, Mode
//   - Evidence:     pkg/evidence/export.go -> ExportBundle
//                   pkg/evidence/evidence.go -> Evidence
//                   pkg/evidence/checkpoint.go -> Checkpoint

/** Run mode reported by the backend policy (pkg/runmode/runmode.go). */
export type RunMode = 'production' | 'simulation' | 'degraded'

/** Per-subsystem backing mode (pkg/capability/registry.go). */
export type CapabilityMode = 'real' | 'simulated' | 'disabled'

/** A single subsystem's capability record — mirrors capability.Backend. */
export interface CapabilityBackend {
  component: string
  mode: CapabilityMode
  driver: string
  detail?: string
  registered_at?: string
}

/** GET /api/v1/capabilities response — mirrors handleCapabilities(). */
export interface CapabilitiesResponse {
  run_mode: RunMode
  all_real: boolean
  simulated_count: number
  backends: CapabilityBackend[]
  simulated: CapabilityBackend[]
}

/**
 * Envelope used across the console: every data payload carries an honest
 * `source` marker so the UI can loudly disclose when it is showing MOCK data
 * instead of a live backend response. This is the anti "silent-fake" contract.
 */
export interface DataEnvelope<T> {
  data: T
  /** 'api' = live backend, 'mock' = local fallback (backend unreachable). */
  source: 'api' | 'mock'
  /** Human-readable reason recorded when we fell back to mock. */
  reason?: string
  fetchedAt: string
}

// --- Evidence chain contracts (pkg/evidence) --------------------------------

/** One tamper-evident receipt — mirrors evidence.Evidence. */
export interface EvidenceRecord {
  id: string
  seq: number
  prev_hash: string
  timestamp: string
  action?: string
  module?: string
  input_hash?: string
  output_hash?: string
  payload?: unknown
  hash: string
  signature: string
  key_id: string
  log_entry?: unknown
}

/** Signed tree head — mirrors evidence.Checkpoint. */
export interface EvidenceCheckpoint {
  origin: string
  tree_size: number
  root_hash: string
  timestamp: string
  key_id: string
  signature: string
}

/** Portable chain snapshot — mirrors evidence.ExportBundle. */
export interface EvidenceBundle {
  key_id: string
  public_key_pem?: string
  keys?: { key_id: string; pem: string }[]
  run_mode?: string
  exported_at?: string
  count: number
  checkpoint?: EvidenceCheckpoint
  records: EvidenceRecord[]
}

// --- GPU utilization (no backend endpoint yet -> always mock, labeled) ------

export interface GpuCell {
  node: string
  gpuIndex: number
  utilization: number // 0-100
  memoryUsed: number // GiB
  memoryTotal: number // GiB
  temperature: number // Celsius
}

export interface GpuGrid {
  nodes: string[]
  gpusPerNode: number
  cells: GpuCell[]
}

// --- Cloud Provider Management (pkg/cloudprovider) --------------------------

export interface CloudProvider {
  name: string
  vendor: 'aws' | 'azure' | 'gcp' | 'tencent' | 'huawei' | 'alibaba'
  region?: string
  capabilities: string[]
  mode: CapabilityMode
  driver: string
  detail?: string
  lastVerified?: string
}

export interface CloudProviderList {
  providers: CloudProvider[]
  totalReal: number
  totalSimulated: number
}

// --- Event Bus Metrics (pkg/eventbus) ---------------------------------------

export interface EventBusMetrics {
  eventsPerSec: number
  avgLatencyMs: number
  hopDistribution: { hops: number; count: number }[]
  signatureOverheadMs: number
  consumerLag: number
}

// --- Config Center (pkg/config) ---------------------------------------------

export interface ConfigCenterState {
  flags: { key: string; value: string; updatedAt: string }[]
  crdtConvergence: { shard: string; version: number; converged: boolean }[]
  queryLatencyMs: number
  sealedKeys: number
}

// --- Training Jobs (pkg/training) -------------------------------------------

export interface TrainingJob {
  id: string
  name: string
  gangSize: number
  gpuCount: number
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  startTime?: string
  endTime?: string
  metrics?: Record<string, number>
}

export interface JobQueue {
  jobs: TrainingJob[]
  admitted: number
  rejected: number
}

// --- MLOps Experiment Tracking (pkg/mlops) ----------------------------------

export interface ExperimentRun {
  id: string
  name: string
  metrics: Record<string, number>
  provenanceVerified: boolean
  createdAt: string
}

export interface ExperimentList {
  runs: ExperimentRun[]
  totalRuns: number
}

// --- Model Drift Detection (pkg/mlops) --------------------------------------

export interface DriftPoint {
  timestamp: string
  psi: number
  ks: number
  thresholdWarn: boolean
  thresholdBreach: boolean
}

export interface DriftStats {
  points: DriftPoint[]
  maxPsi: number
  maxKs: number
  breachedAt?: string
}

// --- GPU Topology Scheduling (pkg/scheduler/dense-k-subgraph) ---------------

export interface SchedulerResult {
  solver: string
  qualityRatio: number
  latencyNs: number
  throughputGbps: number
}

export interface SchedulerStats {
  results: SchedulerResult[]
  meanApproxRatio: number
  worstApproxRatio: number
}

// --- Exact Quantile (pkg/quantile/TailExact) --------------------------------

export interface QuantileComparison {
  estimator: string
  absErr: { p50: number; p90: number; p99: number; p999: number }
  memoryBytes: number
  insertOpsPerSec: number
}

export interface QuantileBenchmark {
  comparisons: QuantileComparison[]
  dataset: string
}

// --- Streaming Anomaly Detection (pkg/anomaly) ------------------------------

export interface AnomalyPoint {
  timestamp: number
  mahalanobisDistance: number
  chiSquareThreshold: number
  isAnomaly: boolean
}

export interface AnomalySeries {
  points: AnomalyPoint[]
  warmupN: number
  dimensions: number
}

// --- Delta Sync (pkg/deltasync/FastCDC) -------------------------------------

export interface DeltaSyncResult {
  method: string
  amplificationFactor: number
  throughputMs: number
  dedupRate: number
}

export interface DeltaSyncBenchmark {
  results: DeltaSyncResult[]
  scenario: string
}

// --- Causal Alert Correlation (pkg/correlation) -----------------------------

export interface AlertCorrelationResult {
  threshold: number
  compressionRatio: number
  misSuppressRate: number
  rootsCount: number
}

export interface AlertCorrelationSweep {
  results: AlertCorrelationResult[]
}
