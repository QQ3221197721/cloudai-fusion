import axios from 'axios'
import ReactECharts from 'echarts-for-react'
import { Card, Alert, Tag, Progress } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import type { DataEnvelope } from '../types'

// GpuMigrationDashboard — M3 dashboard. Shows the CRIU + InfiniBand cross-node
// GPU live-migration queue and per-job phase/status
// (pkg/scheduler/complete_gpu_migration.go).
//
// DATA SOURCE: attempts GET /api/v1/gpu/migrate first; on failure falls back to
// a clearly LABELED mock envelope. Mock timings mirror what
// docs/final-hardware-validation/m3_migration_validation.sh measures
// (checkpoint + RDMA transfer + restore windows) on a 2×A100 IB cluster.

const DEFAULT_TIMEOUT_MS = 6_000
const ENDPOINT = '/api/v1/gpu/migrate'

// Phases mirror the migrate() steps in complete_gpu_migration.go.
type MigrationPhase = 'queued' | 'checkpointing' | 'transferring' | 'restoring' | 'completed' | 'failed'

interface MigrationJob {
  id: string
  workload: string
  sourceNode: string
  targetNode: string
  phase: MigrationPhase
  progressPct: number
  checkpointSec: number
  transferSec: number
  restoreSec: number
  rdmaGbps: number
}

interface MigrationState {
  criuVersion: string
  rdmaBandwidthGbps: number
  jobs: MigrationJob[]
}

const PHASE_COLOR: Record<MigrationPhase, string> = {
  queued: 'default',
  checkpointing: 'blue',
  transferring: 'geekblue',
  restoring: 'purple',
  completed: 'green',
  failed: 'red',
}

function parse(raw: unknown): MigrationState {
  const j = (raw ?? {}) as Record<string, unknown>
  return {
    criuVersion: String(j.criuVersion ?? j.criu_version ?? 'unknown'),
    rdmaBandwidthGbps: Number(j.rdmaBandwidthGbps ?? j.rdma_bandwidth_gbps ?? 0),
    jobs: Array.isArray(j.jobs) ? (j.jobs as MigrationJob[]) : [],
  }
}

function mockState(): MigrationState {
  return {
    criuVersion: '3.19',
    rdmaBandwidthGbps: 200, // Azure ND96asr_v4 HDR IB / AWS EFA
    jobs: [
      { id: 'mig-1042', workload: 'train:gpt-neo-1.3b', sourceNode: 'a100-a', targetNode: 'a100-b', phase: 'completed', progressPct: 100, checkpointSec: 4.20, transferSec: 1.85, restoreSec: 3.10, rdmaGbps: 196.4 },
      { id: 'mig-1043', workload: 'inference:llama-7b', sourceNode: 'a100-a', targetNode: 'a100-b', phase: 'transferring', progressPct: 62, checkpointSec: 5.05, transferSec: 0, restoreSec: 0, rdmaGbps: 191.2 },
      { id: 'mig-1044', workload: 'batch:embeddings', sourceNode: 'a100-b', targetNode: 'a100-a', phase: 'checkpointing', progressPct: 18, checkpointSec: 0, transferSec: 0, restoreSec: 0, rdmaGbps: 0 },
      { id: 'mig-1045', workload: 'train:resnet50', sourceNode: 'a100-a', targetNode: 'a100-b', phase: 'queued', progressPct: 0, checkpointSec: 0, transferSec: 0, restoreSec: 0, rdmaGbps: 0 },
    ],
  }
}

function loadMigrationState(): Promise<DataEnvelope<MigrationState>> {
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
        data: mockState(),
        source: 'mock' as const,
        reason: `Backend endpoint ${ENDPOINT} unreachable — showing a migration queue shaped to complete_gpu_migration.go. Real windows come from m3_migration_validation.sh on a 2×A100 IB cluster.`,
        fetchedAt: new Date().toISOString(),
      }
    })
}

// Stacked bar of the checkpoint/transfer/restore window for completed jobs.
function buildWindowChart(jobs: MigrationJob[]): Record<string, unknown> {
  const done = jobs.filter((j) => j.phase === 'completed')
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { data: ['Checkpoint', 'Transfer', 'Restore'], textStyle: { color: '#aab' } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'value', name: 'seconds', axisLabel: { color: '#aab' } },
    yAxis: { type: 'category', data: done.map((j) => j.id), axisLabel: { color: '#aab' } },
    series: [
      { name: 'Checkpoint', type: 'bar', stack: 'w', data: done.map((j) => j.checkpointSec), itemStyle: { color: '#1677ff' } },
      { name: 'Transfer', type: 'bar', stack: 'w', data: done.map((j) => j.transferSec), itemStyle: { color: '#722ed1' } },
      { name: 'Restore', type: 'bar', stack: 'w', data: done.map((j) => j.restoreSec), itemStyle: { color: '#52c41a' } },
    ],
  }
}

export function GpuMigrationDashboard(): JSX.Element {
  return (
    <DashboardPage<MigrationState>
      title="GPU Live Migration"
      subtitle="CRIU + InfiniBand cross-node migration queue & status (M3)"
      backendModule="pkg/scheduler/complete_gpu_migration.go"
      loader={loadMigrationState}
      isEmpty={(data) => data.jobs.length === 0}
      dataSourceNote={
        <>
          Live data endpoint: <code>{ENDPOINT}</code>. On-hardware verification runs via{' '}
          <code>docs/final-hardware-validation/m3_migration_validation.sh</code> (2×A100 in one placement group).
        </>
      }
      children={(data): JSX.Element => (
        <>
          <Alert
            type="info"
            showIcon
            message={
              <>
                CRIU <Tag>{data.criuVersion}</Tag> · RDMA fabric <Tag color="geekblue">{data.rdmaBandwidthGbps} Gbps</Tag> ·{' '}
                {data.jobs.length} job(s) in queue
              </>
            }
          />

          <Card title="Completed migration windows" style={{ marginTop: 16 }}>
            <ReactECharts option={buildWindowChart(data.jobs)} style={{ height: 260 }} />
          </Card>

          <Card title="Migration queue" style={{ marginTop: 16 }}>
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr>
                  <th style={{ textAlign: 'left' }}>Job</th>
                  <th>Workload</th>
                  <th>Route</th>
                  <th>Phase</th>
                  <th style={{ width: 160 }}>Progress</th>
                  <th>RDMA (Gbps)</th>
                  <th>Total (s)</th>
                </tr>
              </thead>
              <tbody>
                {data.jobs.map((j) => {
                  const total = j.checkpointSec + j.transferSec + j.restoreSec
                  return (
                    <tr key={j.id}>
                      <td style={{ fontFamily: 'monospace' }}>{j.id}</td>
                      <td>{j.workload}</td>
                      <td style={{ textAlign: 'center' }}>{j.sourceNode} → {j.targetNode}</td>
                      <td style={{ textAlign: 'center' }}>
                        <Tag color={PHASE_COLOR[j.phase]}>{j.phase}</Tag>
                      </td>
                      <td>
                        <Progress
                          percent={j.progressPct}
                          size="small"
                          status={j.phase === 'failed' ? 'exception' : j.phase === 'completed' ? 'success' : 'active'}
                        />
                      </td>
                      <td style={{ textAlign: 'center' }}>{j.rdmaGbps > 0 ? j.rdmaGbps.toFixed(1) : '—'}</td>
                      <td style={{ textAlign: 'center' }}>{total > 0 ? total.toFixed(2) : '—'}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </Card>
        </>
      )}
    />
  )
}
