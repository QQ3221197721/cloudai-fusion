// moduleData.ts — Honest data loaders for the 11 module dashboards.
//
// Every loader follows the same anti "silent-fake" contract used by
// getCapabilities() in ./api.ts: it attempts the documented backend endpoint
// first and, when unreachable, falls back to a clearly labeled MOCK payload
// wrapped in DataEnvelope<T> (source: 'mock' + reason). The UI then renders a
// MockDataBanner / [MOCK] tag so an operator is never misled.
//
// IMPORTANT — data provenance:
//   * The 5 new-algorithm dashboards (scheduler / quantile / anomaly /
//     deltasync / correlation) are seeded with the REAL measured numbers
//     recorded in cloudai-fusion/docs/algorithm-*.md. They are static (the
//     numbers do not change between renders) but they are NOT invented — each
//     page cites its source doc + the Go package that produced them.
//   * The 6 verified-backend dashboards mirror the backend contract shapes
//     (pkg/cloudprovider, pkg/eventbus, pkg/config, pkg/training, pkg/mlops).
//     No HTTP endpoint is wired for them yet, so the numbers are illustrative
//     mock values shaped to the real structs, and are labeled as such.
import axios from 'axios'
import type {
  DataEnvelope,
  CloudProviderList,
  EventBusMetrics,
  ConfigCenterState,
  JobQueue,
  ExperimentList,
  DriftStats,
  SchedulerStats,
  QuantileBenchmark,
  AnomalySeries,
  DeltaSyncBenchmark,
  AlertCorrelationSweep,
  ApiClientGenBenchmark,
  DocGenBenchmark,
} from '../types'

const DEFAULT_TIMEOUT_MS = 6_000

function nowIso(): string {
  return new Date().toISOString()
}

// tryFetch attempts a live GET and, on any failure, returns a labeled mock
// envelope produced by the supplied factory. Mirrors getCapabilities().
async function tryFetch<T>(
  endpoint: string,
  parse: (raw: unknown) => T,
  mockFactory: () => T,
): Promise<DataEnvelope<T>> {
  try {
    const response = await axios.get(endpoint, { timeout: DEFAULT_TIMEOUT_MS })
    return { data: parse(response.data), source: 'api', fetchedAt: nowIso() }
  } catch (err) {
    console.warn(`GET ${endpoint} failed; using labeled mock.`, err)
    return {
      data: mockFactory(),
      source: 'mock',
      reason: `Backend endpoint ${endpoint} unreachable at runtime.`,
      fetchedAt: nowIso(),
    }
  }
}

// --- 1. Cloud Provider Management (pkg/cloudprovider) -----------------------
export function getCloudProviders(): Promise<DataEnvelope<CloudProviderList>> {
  return tryFetch<CloudProviderList>(
    '/api/v1/providers',
    (raw) => raw as CloudProviderList,
    () => {
      const providers: CloudProviderList['providers'] = [
        { name: 'AWS us-east-1', vendor: 'aws', region: 'us-east-1', capabilities: ['ec2', 's3', 'eks'], mode: 'real', driver: 'aws-sdk-go-v2', detail: 'credentials present', lastVerified: nowIso() },
        { name: 'Azure westeurope', vendor: 'azure', region: 'westeurope', capabilities: ['vm', 'blob', 'aks'], mode: 'real', driver: 'azure-sdk', detail: 'credentials present', lastVerified: nowIso() },
        { name: 'GCP us-central1', vendor: 'gcp', region: 'us-central1', capabilities: ['gce', 'gcs', 'gke'], mode: 'simulated', driver: 'sim', detail: 'no credentials — degraded to simulation', lastVerified: nowIso() },
        { name: 'Tencent ap-guangzhou', vendor: 'tencent', region: 'ap-guangzhou', capabilities: ['cvm', 'cos'], mode: 'real', driver: 'tencentcloud-sdk', detail: 'credentials present', lastVerified: nowIso() },
        { name: 'Huawei cn-north-4', vendor: 'huawei', region: 'cn-north-4', capabilities: ['ecs', 'obs'], mode: 'simulated', driver: 'sim', detail: 'no credentials — degraded to simulation', lastVerified: nowIso() },
        { name: 'Alibaba cn-hangzhou', vendor: 'alibaba', region: 'cn-hangzhou', capabilities: ['ecs', 'oss', 'ack'], mode: 'real', driver: 'alibaba-cloud-sdk', detail: 'credentials present', lastVerified: nowIso() },
      ]
      const totalSimulated = providers.filter((p) => p.mode === 'simulated').length
      return { providers, totalReal: providers.length - totalSimulated, totalSimulated }
    },
  )
}

// --- 2. Event Fabric Throughput (pkg/eventbus) ------------------------------
export function getEventBusMetrics(): Promise<DataEnvelope<EventBusMetrics>> {
  return tryFetch<EventBusMetrics>(
    '/api/v1/eventbus/metrics',
    (raw) => raw as EventBusMetrics,
    () => ({
      eventsPerSec: 48_200,
      avgLatencyMs: 1.8,
      hopDistribution: [
        { hops: 1, count: 31_400 },
        { hops: 2, count: 12_800 },
        { hops: 3, count: 3_100 },
        { hops: 4, count: 720 },
        { hops: 5, count: 180 },
      ],
      signatureOverheadMs: 0.42,
      consumerLag: 37,
    }),
  )
}

// --- 3. Config Center (pkg/config) ------------------------------------------
export function getConfigCenter(): Promise<DataEnvelope<ConfigCenterState>> {
  return tryFetch<ConfigCenterState>(
    '/api/v1/config/state',
    (raw) => raw as ConfigCenterState,
    () => ({
      flags: [
        { key: 'scheduler.gpu_topology_aware', value: 'true', updatedAt: nowIso() },
        { key: 'evidence.rekor_anchor', value: 'enabled', updatedAt: nowIso() },
        { key: 'anomaly.chi_square_alpha', value: '0.01', updatedAt: nowIso() },
        { key: 'deltasync.fastcdc_target', value: '8192', updatedAt: nowIso() },
      ],
      crdtConvergence: [
        { shard: 'shard-0', version: 1287, converged: true },
        { shard: 'shard-1', version: 1287, converged: true },
        { shard: 'shard-2', version: 1286, converged: false },
        { shard: 'shard-3', version: 1287, converged: true },
      ],
      queryLatencyMs: 0.31,
      sealedKeys: 42,
    }),
  )
}

// --- 4. Training Jobs (pkg/training) ----------------------------------------
export function getTrainingJobs(): Promise<DataEnvelope<JobQueue>> {
  return tryFetch<JobQueue>(
    '/api/v1/training/jobs',
    (raw) => raw as JobQueue,
    () => ({
      jobs: [
        { id: 'job-7f21', name: 'llama3-8b-sft', gangSize: 8, gpuCount: 64, status: 'running', startTime: nowIso(), metrics: { loss: 1.842, lr: 0.00012 } },
        { id: 'job-3a09', name: 'resnet50-distill', gangSize: 4, gpuCount: 16, status: 'succeeded', startTime: nowIso(), endTime: nowIso(), metrics: { top1: 0.762 } },
        { id: 'job-9c4d', name: 'bert-large-pretrain', gangSize: 16, gpuCount: 128, status: 'pending', metrics: {} },
        { id: 'job-1b88', name: 'stable-diffusion-lora', gangSize: 2, gpuCount: 8, status: 'failed', startTime: nowIso(), endTime: nowIso(), metrics: {} },
      ] as any[], // workaround: metrics is Record<string, number> but TS infers union type with undefined
      admitted: 3,
      rejected: 1,
    }),
  )
}

// --- 5. Experiment Tracking (pkg/mlops) -------------------------------------
export function getExperiments(): Promise<DataEnvelope<ExperimentList>> {
  return tryFetch<ExperimentList>(
    '/api/v1/mlops/runs',
    (raw) => raw as ExperimentList,
    () => ({
      runs: Array.from({ length: 8 }, (_, i) => ({
        id: `run-${(i + 1).toString().padStart(3, '0')}`,
        name: `sweep-lr-${(0.0001 * (i + 1)).toFixed(4)}`,
        metrics: {
          accuracy: Number((0.71 + i * 0.021).toFixed(4)),
          loss: Number((2.1 - i * 0.14).toFixed(4)),
        },
        provenanceVerified: i !== 5, // run-006 has a broken Ed25519 provenance signature
        createdAt: nowIso(),
      })),
      totalRuns: 8,
    }),
  )
}

// --- 6. Model Drift (pkg/mlops) ---------------------------------------------
const PSI_WARN = 0.1
const PSI_BREACH = 0.25
export function getDriftStats(): Promise<DataEnvelope<DriftStats>> {
  return tryFetch<DriftStats>(
    '/api/v1/mlops/drift',
    (raw) => raw as DriftStats,
    () => {
      const base = Date.now() - 30 * 24 * 3600 * 1000
      const points = Array.from({ length: 30 }, (_, i) => {
        // Deterministic slow-drift ramp: PSI/KS climb over the window.
        const psi = Number((0.02 + i * 0.009 + (i > 22 ? (i - 22) * 0.02 : 0)).toFixed(4))
        const ks = Number((0.03 + i * 0.006).toFixed(4))
        return {
          timestamp: new Date(base + i * 24 * 3600 * 1000).toISOString(),
          psi,
          ks,
          thresholdWarn: psi >= PSI_WARN && psi < PSI_BREACH,
          thresholdBreach: psi >= PSI_BREACH,
        }
      })
      const maxPsi = Math.max(...points.map((p) => p.psi))
      const maxKs = Math.max(...points.map((p) => p.ks))
      const breached = points.find((p) => p.thresholdBreach)
      return { points, maxPsi, maxKs, breachedAt: breached?.timestamp }
    },
  )
}

// --- 7. GPU Topology Scheduling (pkg/scheduler dense-k-subgraph) ------------
// Source: docs/algorithm-gpu-topology-scheduling.md §5.1 (1000 random topos,
// seed 20260818). Numbers are REAL measured quality ratios / latencies.
export function getSchedulerStats(): Promise<DataEnvelope<SchedulerStats>> {
  return tryFetch<SchedulerStats>(
    '/api/v1/scheduler/topology/benchmark',
    (raw) => raw as SchedulerStats,
    () => ({
      results: [
        { solver: 'exact-bnb', qualityRatio: 1.0, latencyNs: 212724, throughputGbps: 4105.5 },
        { solver: 'greedy-2opt', qualityRatio: 0.99995, latencyNs: 12976, throughputGbps: 4105.3 },
        { solver: 'binpack', qualityRatio: 0.51058, latencyNs: 998, throughputGbps: 2219.0 },
        { solver: 'k8s-default', qualityRatio: 0.50516, latencyNs: 999, throughputGbps: 2209.1 },
        { solver: 'first-fit', qualityRatio: 0.51618, latencyNs: 0, throughputGbps: 2253.5 },
        { solver: 'random', qualityRatio: 0.49772, latencyNs: 0, throughputGbps: 2197.1 },
      ],
      meanApproxRatio: 0.999954,
      worstApproxRatio: 0.972027,
    }),
  )
}

// --- 8. Exact Quantile (pkg/quantile TailExact) -----------------------------
// Source: docs/algorithm-exact-quantile.md — Normal N(0,1), n=20000.
export function getQuantileBenchmark(): Promise<DataEnvelope<QuantileBenchmark>> {
  return tryFetch<QuantileBenchmark>(
    '/api/v1/quantile/benchmark',
    (raw) => raw as QuantileBenchmark,
    () => ({
      dataset: 'Normal N(0,1), n=20000',
      comparisons: [
        { estimator: 'Exact(treap)', absErr: { p50: 0, p90: 0, p99: 0, p999: 0 }, memoryBytes: 960_000, insertOpsPerSec: 5_300_000 },
        { estimator: 'GK(eps=0.001)', absErr: { p50: 0.002, p90: 0.0, p99: 0.006, p999: 0.046 }, memoryBytes: 48_000, insertOpsPerSec: 3_700_000 },
        { estimator: 'KLL(k=128)', absErr: { p50: 0.007, p90: 0.042, p99: 0.022, p999: 0.646 }, memoryBytes: 14_000, insertOpsPerSec: 12_300_000 },
        { estimator: 't-digest(delta=200)', absErr: { p50: 0.018, p90: 0.009, p99: 0.013, p999: 0.017 }, memoryBytes: 49_000, insertOpsPerSec: 6_300_000 },
        { estimator: 'TailExact(K=500)', absErr: { p50: 0.013, p90: 0.016, p99: 0.0, p999: 0.0 }, memoryBytes: 14_000, insertOpsPerSec: 4_800_000 },
      ],
    }),
  )
}

// --- 9. Streaming Anomaly Detection (pkg/anomaly) ---------------------------
// Method is real (Ledoit-Wolf shrinkage + Cholesky rank-1 update, chi-square
// quantile exact to 1e-10; O(d²)≈20µs @ d=50). The per-point Mahalanobis SERIES
// below is synthesized deterministically for UI validation; the chi-square
// critical value (d=2, α=0.01 → 9.21) and detection rule are the real ones.
const CHI2_D2_ALPHA01 = 9.21
export function getAnomalySeries(): Promise<DataEnvelope<AnomalySeries>> {
  return tryFetch<AnomalySeries>(
    '/api/v1/anomaly/series',
    (raw) => raw as AnomalySeries,
    () => {
      const anomalyIdx = new Set([37, 38, 61, 88, 89, 90])
      const points = Array.from({ length: 120 }, (_, i) => {
        // Deterministic pseudo-random baseline around chi-square expectation (~2),
        // with injected joint-anomaly spikes at the marked indices.
        const s = Math.sin(i * 12.9898) * 43758.5453
        const frac = s - Math.floor(s)
        const baseline = 0.5 + frac * 5.0
        const md = anomalyIdx.has(i) ? 12 + frac * 8 : baseline
        return {
          timestamp: i,
          mahalanobisDistance: Number(md.toFixed(3)),
          chiSquareThreshold: CHI2_D2_ALPHA01,
          isAnomaly: md > CHI2_D2_ALPHA01,
        }
      })
      return { points, warmupN: 10, dimensions: 2 }
    },
  )
}

// --- 10. Delta Sync (pkg/deltasync FastCDC) ---------------------------------
// Source: docs/algorithm-cdc-delta-sync.md §1.5 & §2.2 — Head Insert scenario,
// baseSize=256KB. Amplification/throughput numbers are REAL measured values.
export function getDeltaSyncBenchmark(): Promise<DataEnvelope<DeltaSyncBenchmark>> {
  return tryFetch<DeltaSyncBenchmark>(
    '/api/v1/deltasync/benchmark',
    (raw) => raw as DeltaSyncBenchmark,
    () => ({
      scenario: 'Head Insert (1 byte at file head), baseSize=256KB',
      results: [
        // dedupRate for head insert: 1 - retransmitted/total = 1 - 9117/262145 ≈ 0.965
        { method: 'FastCDC', amplificationFactor: 9117, throughputMs: 0.738, dedupRate: 0.965 },
        { method: 'NaiveFixedBlock', amplificationFactor: 262145, throughputMs: 0.310, dedupRate: 0.0 },
      ],
    }),
  )
}

// --- 11. Causal Alert Correlation (pkg/correlation) -------------------------
// Source: docs/algorithm-causal-alert-correlation.md §四 — SuppressThreshold
// sweep. compression/mis-suppress numbers are REAL measured values (0% mis-
// suppress across the whole sweep; recommended operating point = 0.25).
export function getAlertCorrelationSweep(): Promise<DataEnvelope<AlertCorrelationSweep>> {
  return tryFetch<AlertCorrelationSweep>(
    '/api/v1/correlation/sweep',
    (raw) => raw as AlertCorrelationSweep,
    () => ({
      results: [
        { threshold: 0.05, compressionRatio: 0.723, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.10, compressionRatio: 0.650, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.20, compressionRatio: 0.580, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.25, compressionRatio: 0.565, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.30, compressionRatio: 0.535, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.40, compressionRatio: 0.505, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.50, compressionRatio: 0.475, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.70, compressionRatio: 0.420, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 0.90, compressionRatio: 0.380, misSuppressRate: 0.0, rootsCount: 120 },
        { threshold: 1.00, compressionRatio: 0.350, misSuppressRate: 0.0, rootsCount: 120 },
      ],
    }),
  )
}

// --- 12. API Client Generator (pkg/apiclientgen, M40) -----------------------
// Source: REAL `go test ./pkg/apiclientgen/ -bench=. -benchmem -benchtime=5x`
// on Intel Core Ultra 9 275HX (windows/amd64). These are the raw ns/op, B/op
// and allocs/op reported by the Go benchmark harness for the offline
// OpenAPI -> {Go, TypeScript, Python} client pipeline (apiclientgen.
// GenerateFromSpec). No HTTP endpoint is wired for this view yet, so the
// numbers are the static-but-real recorded benchmark, labeled as MOCK source.
export function getApiClientGenBenchmark(): Promise<DataEnvelope<ApiClientGenBenchmark>> {
  return tryFetch<ApiClientGenBenchmark>(
    '/api/v1/apiclientgen/benchmark',
    (raw) => raw as ApiClientGenBenchmark,
    () => ({
      languages: ['go', 'typescript', 'python'],
      benchtime: '5x',
      cpu: 'Intel Core Ultra 9 275HX · windows/amd64',
      rows: [
        { stage: 'ParseJSON', category: 'parse', nsPerOp: 64680, bytesPerOp: 13952, allocsPerOp: 189, note: 'Parse OpenAPI JSON spec' },
        { stage: 'ParseYAML', category: 'parse', nsPerOp: 100460, bytesPerOp: 45510, allocsPerOp: 705, note: 'Parse OpenAPI YAML spec' },
        { stage: 'BuildModel', category: 'model', nsPerOp: 7980, bytesPerOp: 4680, allocsPerOp: 41, note: 'Normalize spec -> intermediate Model' },
        { stage: 'GenerateGo', category: 'generate', target: 'go', nsPerOp: 577900, bytesPerOp: 106265, allocsPerOp: 2466, note: 'Emit Go client (go/format validated)' },
        { stage: 'GenerateTS', category: 'generate', target: 'typescript', nsPerOp: 27760, bytesPerOp: 9030, allocsPerOp: 292, note: 'Emit TypeScript client' },
        { stage: 'GeneratePy', category: 'generate', target: 'python', nsPerOp: 24820, bytesPerOp: 10259, allocsPerOp: 202, note: 'Emit Python client' },
        { stage: 'FullCycle', category: 'fullcycle', nsPerOp: 580380, bytesPerOp: 124897, allocsPerOp: 2696, note: 'Parse -> model -> generate (default Go)' },
      ],
    }),
  )
}

// --- 13. Documentation Generator (pkg/docgen, M43) --------------------------
// Source: REAL `go test ./pkg/docgen/ -bench=. -benchmem -benchtime=5x` on the
// same host. docgen walks real Go ASTs (go/ast + go/doc) and renders Markdown.
// Symbol counts are the harness-reported figures: the medium package is the
// repo's pkg/scheduler (463 symbols); synthetic small/large fixtures have
// 160 / 1920 symbols. Numbers are the recorded benchmark, labeled MOCK source.
export function getDocGenBenchmark(): Promise<DataEnvelope<DocGenBenchmark>> {
  return tryFetch<DocGenBenchmark>(
    '/api/v1/docgen/benchmark',
    (raw) => raw as DocGenBenchmark,
    () => ({
      benchtime: '5x',
      cpu: 'Intel Core Ultra 9 275HX · windows/amd64',
      rows: [
        { stage: 'ParseDir_Small', category: 'parse', symbols: 160, nsPerOp: 953520, bytesPerOp: 199366, allocsPerOp: 4419 },
        { stage: 'ParseDir_Medium', category: 'parse', symbols: 463, nsPerOp: 27504800, bytesPerOp: 6529710, allocsPerOp: 126117 },
        { stage: 'GenerateDoc_Small', category: 'generate', symbols: 160, nsPerOp: 10836700, bytesPerOp: 139417, allocsPerOp: 1463 },
        { stage: 'GenerateDoc_Large', category: 'generate', symbols: 1920, nsPerOp: 13217700, bytesPerOp: 1573563, allocsPerOp: 12456 },
        { stage: 'FullCycle', category: 'fullcycle', symbols: 160, nsPerOp: 1898440, bytesPerOp: 249729, allocsPerOp: 5143 },
      ],
    }),
  )
}
