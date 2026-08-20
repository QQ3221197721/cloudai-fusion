import axios from 'axios'
import ReactECharts from 'echarts-for-react'
import { Card, Alert, Tag } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import type { DataEnvelope } from '../types'

// GpuMigDashboard — M2 dashboard. Renders NVIDIA MIG topology + active
// partitions for the GPU-sharing subsystem (pkg/scheduler/gpu_sharing.go).
//
// DATA SOURCE: attempts the real backend endpoint GET /api/v1/gpu/mig first
// and, when unreachable, falls back to a clearly LABELED mock envelope so the
// UI never lies (same contract as lib/api.ts + lib/moduleData.ts). The mock
// values are shaped to the real MIGInstance struct and to what the on-hardware
// validation (docs/final-hardware-validation/m2_mig_validation.sh) records.

const DEFAULT_TIMEOUT_MS = 6_000

// Mirrors scheduler.MIGInstance / MIGProfile (pkg/scheduler/gpu_sharing.go).
interface MigInstance {
  gpuUuid: string
  giId: number
  ciId: number
  profile: string // e.g. "1g.5gb"
  memoryGb: number
  smSlices: number
  occupied: boolean
  workload: string
}

interface MigGpu {
  index: number
  name: string
  migEnabled: boolean
  instances: MigInstance[]
}

interface MigTopology {
  driverVersion: string
  gpus: MigGpu[]
}

const ENDPOINT = '/api/v1/gpu/mig'

function parse(raw: unknown): MigTopology {
  const j = (raw ?? {}) as Record<string, unknown>
  return {
    driverVersion: String(j.driverVersion ?? j.driver_version ?? 'unknown'),
    gpus: Array.isArray(j.gpus) ? (j.gpus as MigGpu[]) : [],
  }
}

// A100-40GB with MIG enabled: 7× 1g.5gb instances (~4.75GB / 1 SM slice each).
function mockTopology(): MigTopology {
  const mk = (uuidTail: string, gi: number, occupied: boolean, workload: string): MigInstance => ({
    gpuUuid: `MIG-${uuidTail}`,
    giId: gi,
    ciId: gi,
    profile: '1g.5gb',
    memoryGb: 4.75,
    smSlices: 1,
    occupied,
    workload,
  })
  return {
    driverVersion: '550.90.07',
    gpus: [
      {
        index: 0,
        name: 'NVIDIA A100-SXM4-40GB',
        migEnabled: true,
        instances: [
          mk('a1b2c3d4-0', 0, true, 'inference:llama-7b'),
          mk('a1b2c3d4-1', 1, true, 'inference:bert-base'),
          mk('a1b2c3d4-2', 2, true, 'train:resnet50'),
          mk('a1b2c3d4-3', 3, false, ''),
          mk('a1b2c3d4-4', 4, true, 'inference:whisper'),
          mk('a1b2c3d4-5', 5, false, ''),
          mk('a1b2c3d4-6', 6, true, 'batch:embeddings'),
        ],
      },
    ],
  }
}

function loadMigTopology(): Promise<DataEnvelope<MigTopology>> {
  return axios
    .get(ENDPOINT, { timeout: DEFAULT_TIMEOUT_MS })
    .then((res) => {
      // Backend answers with the hardware-transparency envelope
      // { mode, simulated, reason, data }. Unwrap it and carry the honesty
      // markers through so the shell can show [SIMULATED - no hardware].
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
        data: mockTopology(),
        source: 'mock' as const,
        reason: `Backend endpoint ${ENDPOINT} unreachable — showing A100 MIG layout shaped to scheduler.MIGInstance. Real numbers come from m2_mig_validation.sh on A100 hardware.`,
        fetchedAt: new Date().toISOString(),
      }
    })
}

function buildOccupancyChart(gpu: MigGpu): Record<string, unknown> {
  const occupied = gpu.instances.filter((i) => i.occupied).length
  const free = gpu.instances.length - occupied
  return {
    tooltip: { trigger: 'item' },
    legend: { bottom: 0, textStyle: { color: '#aab' } },
    series: [
      {
        name: `GPU-${gpu.index} MIG occupancy`,
        type: 'pie',
        radius: ['45%', '70%'],
        label: { color: '#ccd' },
        data: [
          { value: occupied, name: 'Occupied', itemStyle: { color: '#52c41a' } },
          { value: free, name: 'Free', itemStyle: { color: '#434654' } },
        ],
      },
    ],
  }
}

export function GpuMigDashboard(): JSX.Element {
  return (
    <DashboardPage<MigTopology>
      title="GPU MIG Partitions"
      subtitle="NVIDIA Multi-Instance GPU topology & active partitions (M2)"
      backendModule="pkg/scheduler/gpu_sharing.go"
      loader={loadMigTopology}
      isEmpty={(data) => data.gpus.length === 0}
      dataSourceNote={
        <>
          Live data endpoint: <code>{ENDPOINT}</code>. On-hardware verification runs via{' '}
          <code>docs/final-hardware-validation/m2_mig_validation.sh</code> (A100 with MIG enabled).
        </>
      }
      children={(data): JSX.Element => (
        <>
          <Alert
            type="info"
            showIcon
            message={
              <>
                Driver <Tag>{data.driverVersion}</Tag> · {data.gpus.length} MIG-enabled GPU(s) ·{' '}
                {data.gpus.reduce((n, g) => n + g.instances.length, 0)} instances
              </>
            }
          />
          {data.gpus.map((gpu) => (
            <Card key={gpu.index} title={`GPU-${gpu.index} · ${gpu.name}`} style={{ marginTop: 16 }}>
              <ReactECharts option={buildOccupancyChart(gpu)} style={{ height: 240 }} />
              <table className="mig-table" style={{ width: '100%', marginTop: 12, borderCollapse: 'collapse' }}>
                <thead>
                  <tr>
                    <th style={{ textAlign: 'left' }}>MIG UUID</th>
                    <th>GI/CI</th>
                    <th>Profile</th>
                    <th>Mem (GB)</th>
                    <th>SM slices</th>
                    <th>Workload</th>
                  </tr>
                </thead>
                <tbody>
                  {gpu.instances.map((mi) => (
                    <tr key={mi.gpuUuid}>
                      <td style={{ fontFamily: 'monospace' }}>{mi.gpuUuid}</td>
                      <td style={{ textAlign: 'center' }}>{mi.giId}/{mi.ciId}</td>
                      <td style={{ textAlign: 'center' }}>{mi.profile}</td>
                      <td style={{ textAlign: 'center' }}>{mi.memoryGb.toFixed(2)}</td>
                      <td style={{ textAlign: 'center' }}>{mi.smSlices}</td>
                      <td>
                        {mi.occupied ? (
                          <Tag color="green">{mi.workload}</Tag>
                        ) : (
                          <Tag color="default">idle</Tag>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          ))}
        </>
      )}
    />
  )
}
