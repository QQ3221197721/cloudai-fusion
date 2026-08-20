import axios from 'axios'
import ReactECharts from 'echarts-for-react'
import { Card, Alert, Tag, Statistic, Row, Col } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import type { DataEnvelope } from '../types'

// SgxEnclaveDashboard — M5 dashboard. Lists active SGX enclaves plus the
// host SGX capability (mirrors capability.SGXCapability in
// pkg/capability/detection.go; DetectSGX() stats /dev/sgx_enclave on Linux).
//
// DATA SOURCE: attempts GET /api/v1/sgx/status first; on failure falls back to
// a clearly LABELED mock envelope. Real numbers come from
// docs/final-hardware-validation/m5_sgx_validation.sh on an SGX-enabled CPU
// (Azure DCsv3 / Intel Dev Cloud).

const DEFAULT_TIMEOUT_MS = 6_000
const ENDPOINT = '/api/v1/sgx/status'

// Mirrors capability.SGXCapability plus per-enclave attestation state.
interface SgxCapability {
  available: boolean
  version: string // "2.0" | "1.x"
  epcSizeBytes: number
}

type AttestState = 'attested' | 'pending' | 'failed'

interface Enclave {
  id: string
  workload: string
  mrenclave: string // measurement hash (first bytes shown)
  attestation: AttestState
  epcUsedMb: number
  threads: number
  uptimeSec: number
}

interface SgxStatus {
  capability: SgxCapability
  aesmdRunning: boolean
  enclaves: Enclave[]
}

const ATTEST_COLOR: Record<AttestState, string> = {
  attested: 'green',
  pending: 'gold',
  failed: 'red',
}

function parse(raw: unknown): SgxStatus {
  const j = (raw ?? {}) as Record<string, unknown>
  const cap = (j.capability ?? {}) as Record<string, unknown>
  return {
    capability: {
      available: Boolean(cap.available),
      version: String(cap.version ?? 'unknown'),
      epcSizeBytes: Number(cap.epc_size_bytes ?? cap.epcSizeBytes ?? 0),
    },
    aesmdRunning: Boolean(j.aesmdRunning ?? j.aesmd_running),
    enclaves: Array.isArray(j.enclaves) ? (j.enclaves as Enclave[]) : [],
  }
}

// Azure DC4s_v3: SGX 2.0, sizeable EPC. Two attested enclaves + one pending.
function mockStatus(): SgxStatus {
  return {
    capability: { available: true, version: '2.0', epcSizeBytes: 4 * 1024 * 1024 * 1024 },
    aesmdRunning: true,
    enclaves: [
      { id: 'enc-01', workload: 'kms:seal-unseal', mrenclave: 'e3b0c44298fc1c14…', attestation: 'attested', epcUsedMb: 128, threads: 4, uptimeSec: 8123 },
      { id: 'enc-02', workload: 'inference:private-llm', mrenclave: '9f86d081884c7d65…', attestation: 'attested', epcUsedMb: 512, threads: 8, uptimeSec: 3540 },
      { id: 'enc-03', workload: 'ledger:evidence-signer', mrenclave: '2c26b46b68ffc68f…', attestation: 'pending', epcUsedMb: 64, threads: 2, uptimeSec: 42 },
    ],
  }
}

function loadSgxStatus(): Promise<DataEnvelope<SgxStatus>> {
  return axios
    .get(ENDPOINT, { timeout: DEFAULT_TIMEOUT_MS })
    .then((res) => {
      // Unwrap the { mode, simulated, reason, data } hardware envelope and
      // carry the honesty markers so the shell can disclose simulation.
      const body = (res.data ?? {}) as Record<string, unknown>
      const simulated = Boolean(body.simulated)
      return {
        data: parse(body.data ?? body),
        source: 'api' as const,
        simulated,
        mode: (body.mode as 'real' | 'simulated' | undefined) ?? (simulated ? 'simulated' : 'real'),
        reason: typeof body.reason === 'string' ? body.reason : undefined,
        fetchedAt: new Date().toISOString(),
      }
    })
    .catch((err) => {
      console.warn(`GET ${ENDPOINT} failed; using labeled mock.`, err)
      return {
        data: mockStatus(),
        source: 'mock' as const,
        reason: `Backend endpoint ${ENDPOINT} unreachable — showing enclave list shaped to capability.SGXCapability. Real numbers come from m5_sgx_validation.sh on SGX hardware.`,
        fetchedAt: new Date().toISOString(),
      }
    })
}

function epcGb(bytes: number): number {
  return bytes / (1024 * 1024 * 1024)
}

function buildEpcChart(status: SgxStatus): Record<string, unknown> {
  const used = status.enclaves.reduce((n, e) => n + e.epcUsedMb, 0)
  const totalMb = status.capability.epcSizeBytes / (1024 * 1024)
  const free = Math.max(totalMb - used, 0)
  return {
    tooltip: { trigger: 'item', formatter: '{b}: {c} MB ({d}%)' },
    legend: { bottom: 0, textStyle: { color: '#aab' } },
    series: [
      {
        name: 'EPC usage',
        type: 'pie',
        radius: ['45%', '70%'],
        label: { color: '#ccd' },
        data: [
          { value: Math.round(used), name: 'Used', itemStyle: { color: '#722ed1' } },
          { value: Math.round(free), name: 'Free', itemStyle: { color: '#434654' } },
        ],
      },
    ],
  }
}

export function SgxEnclaveDashboard(): JSX.Element {
  return (
    <DashboardPage<SgxStatus>
      title="SGX Enclaves"
      subtitle="Intel SGX confidential-compute enclave list & metrics (M5)"
      backendModule="pkg/capability/detection.go"
      loader={loadSgxStatus}
      isEmpty={(data) => data.enclaves.length === 0}
      dataSourceNote={
        <>
          Live data endpoint: <code>{ENDPOINT}</code>. On-hardware verification runs via{' '}
          <code>docs/final-hardware-validation/m5_sgx_validation.sh</code> (Azure DCsv3 / Intel Dev Cloud).
        </>
      }
      children={(data): JSX.Element => (
        <>
          {!data.capability.available && (
            <Alert
              type="warning"
              showIcon
              message="Host reports SGX unavailable — /dev/sgx_enclave not present. Enclave metrics below are illustrative."
            />
          )}

          <Row gutter={16} style={{ marginTop: 8 }}>
            <Col span={6}>
              <Card>
                <Statistic title="SGX Version" value={data.capability.version} />
              </Card>
            </Col>
            <Col span={6}>
              <Card>
                <Statistic title="EPC Size (GB)" value={epcGb(data.capability.epcSizeBytes)} precision={2} />
              </Card>
            </Col>
            <Col span={6}>
              <Card>
                <Statistic title="Active Enclaves" value={data.enclaves.length} />
              </Card>
            </Col>
            <Col span={6}>
              <Card>
                <Statistic
                  title="AESMD (attestation)"
                  value={data.aesmdRunning ? 'running' : 'stopped'}
                  valueStyle={{ color: data.aesmdRunning ? '#52c41a' : '#ff4d4f' }}
                />
              </Card>
            </Col>
          </Row>

          <Card title="EPC (Enclave Page Cache) usage" style={{ marginTop: 16 }}>
            <ReactECharts option={buildEpcChart(data)} style={{ height: 240 }} />
          </Card>

          <Card title="Enclaves" style={{ marginTop: 16 }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={{ textAlign: 'left' }}>Enclave</th>
                  <th>Workload</th>
                  <th>MRENCLAVE</th>
                  <th>Attestation</th>
                  <th>EPC (MB)</th>
                  <th>Threads</th>
                  <th>Uptime (s)</th>
                </tr>
              </thead>
              <tbody>
                {data.enclaves.map((e) => (
                  <tr key={e.id}>
                    <td style={{ fontFamily: 'monospace' }}>{e.id}</td>
                    <td>{e.workload}</td>
                    <td style={{ fontFamily: 'monospace' }}>{e.mrenclave}</td>
                    <td style={{ textAlign: 'center' }}>
                      <Tag color={ATTEST_COLOR[e.attestation]}>{e.attestation}</Tag>
                    </td>
                    <td style={{ textAlign: 'center' }}>{e.epcUsedMb}</td>
                    <td style={{ textAlign: 'center' }}>{e.threads}</td>
                    <td style={{ textAlign: 'center' }}>{e.uptimeSec.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        </>
      )}
    />
  )
}
