// api.ts — Honest API client for CloudAI Fusion backends with clear MOCK fallbacks.
// Every fetch wraps responses in DataEnvelope to guarantee the UI never lies.
import axios from 'axios'
import {
  CapabilitiesResponse,
  CapabilityBackend,
  EvidenceBundle,
  DataEnvelope,
} from '../types'

const DEFAULT_TIMEOUT_MS = 6_000

// Produce honest mock capabilities that match backend shape when offline.
function produceMockCapabilities(): CapabilitiesResponse {
  const now = new Date().toISOString()
  const backends: CapabilityBackend[] = [
    { component: 'cache', mode: 'real', driver: 'redis', detail: 'connected', registered_at: now },
    { component: 'messaging', mode: 'simulated', driver: 'memory', detail: 'nats driver unavailable', registered_at: now },
    { component: 'scheduler.nodes', mode: 'real', driver: 'k8s', detail: 'cluster-ready', registered_at: now },
    { component: 'mesh.evidence', mode: 'real', driver: 'rekor', detail: 'online', registered_at: now },
    { component: 'edgeautonomy', mode: 'simulated', driver: 'sim', detail: 'local-simulation', registered_at: now },
    { component: 'wasm.sandbox', mode: 'real', driver: 'wazero', detail: 'hot-swap-enabled', registered_at: now },
    { component: 'zkp.prover', mode: 'real', driver: 'gnark', detail: 'groth16', registered_at: now },
  ]
  const simulated = backends.filter((b) => b.mode === 'simulated')
  return {
    run_mode: 'simulation', // offline → conservative default
    all_real: simulated.length === 0,
    simulated_count: simulated.length,
    backends,
    simulated,
  }
}

// Normalize an arbitrary JSON payload into a strict CapabilitiesResponse.
function parseCapabilities(json: unknown): CapabilitiesResponse {
  const j = (json ?? {}) as Record<string, unknown>
  return {
    run_mode: (j.run_mode as CapabilitiesResponse['run_mode']) ?? 'simulation',
    all_real: Boolean(j.all_real),
    simulated_count: Number(j.simulated_count ?? 0),
    backends: Array.isArray(j.backends) ? (j.backends as CapabilityBackend[]) : [],
    simulated: Array.isArray(j.simulated) ? (j.simulated as CapabilityBackend[]) : [],
  }
}

// Real GET /api/v1/capabilities call wrapped with an honest envelope. When the
// backend is unreachable we fall back to labeled mock data and record why.
export async function getCapabilities(): Promise<DataEnvelope<CapabilitiesResponse>> {
  try {
    const response = await axios.get('/api/v1/capabilities', { timeout: DEFAULT_TIMEOUT_MS })
    return {
      data: parseCapabilities(response.data),
      source: 'api',
      fetchedAt: new Date().toISOString(),
    }
  } catch (err) {
    console.warn('GET /api/v1/capabilities failed; using labeled mock.', err)
    return {
      data: produceMockCapabilities(),
      source: 'mock',
      reason: 'Backend unreachable at runtime.',
      fetchedAt: new Date().toISOString(),
    }
  }
}

// isBase64 does a browser-safe base64 validity check (no Node Buffer).
function isBase64(value: string): boolean {
  if (typeof value !== 'string' || value.length === 0) return false
  try {
    // atob throws on malformed base64.
    atob(value)
    return /^[A-Za-z0-9+/]+={0,2}$/.test(value)
  } catch {
    return false
  }
}

export interface EvidenceValidationDetails {
  keyId?: string
  count: number
  merkleRoot?: string
  checkpointPresent: boolean
  firstSeq: number | null
  lastSeq: number | null
  brokenChainIndex?: number
  validSignatures: number
  invalidSignatures: number
}

export interface EvidenceValidationResult {
  success: boolean
  error?: string
  details: EvidenceValidationDetails
}

function emptyDetails(): EvidenceValidationDetails {
  return { count: 0, checkpointPresent: false, firstSeq: null, lastSeq: null, validSignatures: 0, invalidSignatures: 0 }
}

// validateEvidenceBundle performs browser-side structural verification of an
// exported chain: JSON shape, hash-chain linkage (record.prev_hash must equal
// the previous record's hash), and base64 signature encoding. It reports the
// exact broken index instead of a vague "failed".
export async function validateEvidenceBundle(bundleJson: unknown): Promise<EvidenceValidationResult> {
  try {
    const bundle = bundleJson as EvidenceBundle
    if (!bundle || !Array.isArray(bundle.records)) {
      return { success: false, error: 'Invalid bundle: missing "records" array.', details: emptyDetails() }
    }

    const records = bundle.records
    const details: EvidenceValidationDetails = {
      keyId: bundle.key_id,
      count: typeof bundle.count === 'number' ? bundle.count : records.length,
      merkleRoot: bundle.checkpoint?.root_hash,
      checkpointPresent: !!bundle.checkpoint,
      firstSeq: records.length ? records[0].seq : null,
      lastSeq: records.length ? records[records.length - 1].seq : null,
      validSignatures: 0,
      invalidSignatures: 0,
    }

    if (records.length === 0) {
      return { success: true, details }
    }

    // 1) Genesis link: the first record must chain to the "genesis" sentinel
    //    (matches pkg/evidence GenesisPrevHash = "genesis").
    if (records[0].prev_hash !== 'genesis') {
      return {
        success: false,
        error: `genesis record missing: records[0].prev_hash must be "genesis" but was "${records[0].prev_hash}".`,
        details: { ...details, brokenChainIndex: 0 },
      }
    }

    // 2) Hash-chain linkage: record[i].prev_hash === record[i-1].hash. The exact
    //    index + expected/actual hashes are surfaced so an auditor can jump
    //    straight to the tampered record.
    for (let i = 1; i < records.length; i++) {
      const prev = records[i - 1]
      const cur = records[i]
      if (cur.prev_hash !== prev.hash) {
        return {
          success: false,
          error: `Chain broken at record index ${i}: expected prev_hash=${prev.hash}, got ${cur.prev_hash}`,
          details: { ...details, brokenChainIndex: i },
        }
      }
    }

    // 3) Signature encoding check (structural — full Ed25519 verify is a backend concern).
    let valid = 0
    let invalid = 0
    for (const rec of records) {
      if (isBase64(rec.signature)) valid++
      else invalid++
    }
    details.validSignatures = valid
    details.invalidSignatures = invalid

    if (invalid > 0) {
      return {
        success: false,
        error: `${invalid} of ${records.length} records have malformed (non-base64) signatures.`,
        details,
      }
    }

    return { success: true, details }
  } catch (e) {
    return { success: false, error: String(e), details: emptyDetails() }
  }
}
