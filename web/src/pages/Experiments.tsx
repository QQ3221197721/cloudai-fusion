import { Table, Statistic, Row, Col, Tag, Card } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { CheckCircleOutlined, CloseOutlined } from '@ant-design/icons'
import { DashboardPage } from '../components/DashboardPage'
import { getExperiments } from '../lib/moduleData'
import type { ExperimentRun } from '../types'
import './Experiments.css'

export function Experiments(): JSX.Element {
  return (
    <DashboardPage
      title="Experiment Tracking"
      subtitle="Run list, metrics curves, and Ed25519 provenance validation badges from pkg/mlops"
      backendModule="pkg/mlops"
      loader={getExperiments}
      isEmpty={(data) => data.runs.length === 0}
      dataSourceNote="Illustrative mock shaped to pkg/mlops contracts."
      children={(data): JSX.Element => (
        <>
          <Row gutter={[16, 16]} className="stat-row">
            <Col span={8}>
              <div className="stat-box">Total Runs</div>
              <Statistic value={data.totalRuns} />
            </Col>
          </Row>

          <Card title="Runs">
            <Table<ExperimentRun> columns={columns} dataSource={data.runs} rowKey="id" pagination={false} scroll={{ x: 700 }} />
          </Card>
        </>
      )}
    />
  )
}

const columns: ColumnsType<ExperimentRun> = [
  { title: 'Run ID', dataIndex: 'id', key: 'id', width: 100, render: (v) => <code className="run-id">{v}</code> },
  { title: 'Name', dataIndex: 'name', key: 'name', ellipsis: true },
  {
    title: 'Accuracy', dataIndex: ['metrics', 'accuracy'], key: 'accuracy', width: 120,
    render: (_, r) => Number((r.metrics.accuracy ?? 0).toFixed(4)),
  },
  {
    title: 'Loss', dataIndex: ['metrics', 'loss'], key: 'loss', width: 120,
    render: (_, r) => Number((r.metrics.loss ?? 0).toFixed(4)),
  },
  {
    title: 'Provenance', dataIndex: 'provenanceVerified', key: 'provenanceVerified', width: 140,
    render: (verified: boolean) => verified
      ? <Tag color="green"><CheckCircleOutlined style={{ marginRight: 4 }} />[VERIFIED]</Tag>
      : <Tag color="red"><CloseOutlined style={{ marginRight: 4 }} />[BROKEN]</Tag>,
  },
]
