import { Table, Card, Statistic, Tag, Row, Col } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DashboardPage } from '../components/DashboardPage'
import { getCloudProviders } from '../lib/moduleData'
import type { CloudProviderList } from '../types'
import './ProviderManagement.css'

export function ProviderManagement(): JSX.Element {
  return (
    <DashboardPage
      title="Cloud Provider Management"
      subtitle="Real vs simulated backing status for each cloud provider capability"
      backendModule="pkg/cloudprovider"
      loader={getCloudProviders}
      isEmpty={(data) => data.providers.length === 0}
      dataSourceNote="Data is illustrative mock shaped to pkg/cloudprovider contracts. When backend /api/v1/providers is reachable, source will change to [API LIVE]."
      children={(data): JSX.Element => (
        <>
          <Row gutter={[16, 16]} className="dashboard-stats">
            <Col span={8}>
              <Statistic title="Total Providers" value={data.providers.length} valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600 }} />
            </Col>
            <Col span={8}>
              <Statistic title="Live Providers" value={data.totalReal} valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600, color: '#16c784' }} />
            </Col>
            <Col span={8}>
              <Statistic title="Simulated / Degraded" value={data.totalSimulated} valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600, color: '#f5a623' }} />
            </Col>
          </Row>

          <Card bodyStyle={{ padding: 0 }}>
            <Table<CloudProviderList['providers'][number]>
              columns={columns}
              dataSource={data.providers}
              rowKey="name"
              scroll={{ x: 900 }}
              pagination={false}
              className="provider-table"
            />
          </Card>
        </>
      )}
    />
  )
}

const columns: ColumnsType<CloudProviderList['providers'][number]> = [
  {
    title: 'Provider Name',
    dataIndex: 'name',
    key: 'name',
    fixed: 'left',
    width: 180,
    render: (text) => <strong style={{ fontFamily: 'Chakra Petch', color: '#ffffff' }}>{text}</strong>,
  },
  {
    title: 'Vendor',
    dataIndex: 'vendor',
    key: 'vendor',
    width: 100,
    render: (v) => <Tag>{v.toUpperCase()}</Tag>,
  },
  {
    title: 'Region',
    dataIndex: 'region',
    key: 'region',
    width: 140,
    ellipsis: true,
  },
  {
    title: 'Capabilities',
    dataIndex: 'capabilities',
    key: 'capabilities',
    width: 180,
    render: (caps: string[]) => <span>{caps.map((c) => <Tag key={c} className="cap-tag">{c}</Tag>)}</span>,
  },
  {
    title: 'Status',
    dataIndex: 'mode',
    key: 'mode',
    width: 120,
    render: (mode) => {
      const tagColor = mode === 'real' ? 'green' : mode === 'simulated' ? 'orange' : 'default'
      const label = mode === 'real' ? '[REAL]' : mode === 'simulated' ? '[SIMULATED]' : '[DISABLED]'
      return <Tag color={tagColor}>{label}</Tag>
    },
  },
  {
    title: 'Driver',
    dataIndex: 'driver',
    key: 'driver',
    width: 160,
    render: (v) => <code className="driver-code">{v}</code>,
  },
  {
    title: 'Detail',
    dataIndex: 'detail',
    key: 'detail',
    width: 180,
    ellipsis: true,
  },
  {
    title: 'Last Verified',
    dataIndex: 'lastVerified',
    key: 'lastVerified',
    width: 170,
    render: (iso?: string) => iso ? new Date(iso).toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' }) : '-',
  },
]
