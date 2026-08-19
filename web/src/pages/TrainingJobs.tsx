import { Table, Row, Col, Statistic, Tag, Steps } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DashboardPage } from '../components/DashboardPage'
import { getTrainingJobs } from '../lib/moduleData'
import type { TrainingJob } from '../types'
import './TrainingJobs.css'

const STATUS_META: Record<TrainingJob['status'], { color: string; label: string; step: number }> = {
  pending: { color: 'default', label: '[PENDING]', step: 0 },
  running: { color: 'processing', label: '[RUNNING]', step: 1 },
  succeeded: { color: 'green', label: '[SUCCEEDED]', step: 3 },
  failed: { color: 'red', label: '[FAILED]', step: 2 },
}

export function TrainingJobs(): JSX.Element {
  return (
    <DashboardPage
      title="Training Jobs"
      subtitle="Gang-scheduling state machine and admission decisions from pkg/training"
      backendModule="pkg/training"
      loader={getTrainingJobs}
      isEmpty={(data) => data.jobs.length === 0}
      dataSourceNote="Illustrative mock shaped to pkg/training contracts."
      children={(data): JSX.Element => (
        <>
          <Row gutter={[16, 16]} className="stat-row">
            <Col span={8}>
              <div className="stat-box">Total Jobs</div>
              <Statistic value={data.jobs.length} />
            </Col>
            <Col span={8}>
              <div className="stat-box">Admitted</div>
              <Statistic value={data.admitted} valueStyle={{ color: '#16c784' }} />
            </Col>
            <Col span={8}>
              <div className="stat-box">Rejected</div>
              <Statistic value={data.rejected} valueStyle={{ color: '#ff5555' }} />
            </Col>
          </Row>

          <div className="gang-state-machine">
            <Steps
              size="small"
              items={[
                { title: 'Pending' },
                { title: 'Running' },
                { title: 'Failed' },
                { title: 'Succeeded' },
              ]}
              current={-1}
            />
            <p className="sm-note">Gang state machine: all pods admitted atomically (all-or-nothing) before Running.</p>
          </div>

          <Table<TrainingJob> columns={columns} dataSource={data.jobs} rowKey="id" pagination={false} scroll={{ x: 800 }} expandable={{ expandedRowRender: renderExpanded }} />
        </>
      )}
    />
  )
}

function renderExpanded(record: TrainingJob): JSX.Element {
  const meta = STATUS_META[record.status]
  return (
    <div className="job-detail">
      <Steps size="small" current={meta.step} status={record.status === 'failed' ? 'error' : 'process'}
        items={[{ title: 'Pending' }, { title: 'Running' }, record.status === 'failed' ? { title: 'Failed' } : { title: 'Running' }, { title: 'Succeeded' }]} />
    </div>
  )
}

const columns: ColumnsType<TrainingJob> = [
  { title: 'Job ID', dataIndex: 'id', key: 'id', width: 120, render: (v) => <code className="job-id">{v}</code> },
  { title: 'Name', dataIndex: 'name', key: 'name', width: 200, ellipsis: true },
  { title: 'Gang Size', dataIndex: 'gangSize', key: 'gangSize', width: 100 },
  { title: 'GPUs', dataIndex: 'gpuCount', key: 'gpuCount', width: 90 },
  {
    title: 'Status', dataIndex: 'status', key: 'status', width: 140,
    render: (s: TrainingJob['status']) => <Tag color={STATUS_META[s].color}>{STATUS_META[s].label}</Tag>,
  },
  {
    title: 'Metrics', dataIndex: 'metrics', key: 'metrics', width: 220,
    render: (m?: Record<string, number>) => m && Object.keys(m).length > 0
      ? <span className="metric-inline">{Object.entries(m).map(([k, v]) => `${k}=${v}`).join('  ')}</span>
      : <span className="metric-empty">—</span>,
  },
]
