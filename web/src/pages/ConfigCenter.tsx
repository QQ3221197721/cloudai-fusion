import ReactECharts from 'echarts-for-react'
import { Table, Statistic, Row, Col, Card } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DashboardPage } from '../components/DashboardPage'
import { getConfigCenter } from '../lib/moduleData'
import type { ConfigCenterState } from '../types'
import './ConfigCenter.css'

export function ConfigCenter(): JSX.Element {
  return (
    <DashboardPage
      title="Config Center"
      subtitle="CRDT convergence state, flag queries, sealed keys from pkg/config"
      backendModule="pkg/config"
      loader={getConfigCenter}
      isEmpty={(data) => data.flags.length === 0}
      dataSourceNote="Illustrative mock shaped to pkg/config contracts."
      children={(data): JSX.Element => (
        <>
          <Row gutter={[16, 16]} className="stat-row">
            <Col span={6}>
              <div className="stat-box">Query Latency</div>
              <Statistic value={data.queryLatencyMs.toFixed(3)} suffix="ms" />
            </Col>
            <Col span={6}>
              <div className="stat-box">Sealed Keys</div>
              <Statistic value={data.sealedKeys} />
            </Col>
            <Col span={12}>
              <ReactECharts option={buildCrdtChart(data.crdtConvergence)} style={{ height: 200 }} />
            </Col>
          </Row>

          <Card title="Active Flags">
            <Table<ConfigCenterState['flags'][number]> columns={columns} dataSource={data.flags} rowKey="key" pagination={false} scroll={{ x: 500 }} />
          </Card>
        </>
      )}
    />
  )
}

const columns: ColumnsType<ConfigCenterState['flags'][number]> = [
  { title: 'Key', dataIndex: 'key', key: 'key', width: 240, ellipsis: true },
  { title: 'Value', dataIndex: 'value', key: 'value', width: 140 },
  { title: 'Updated At', dataIndex: 'updatedAt', key: 'updatedAt', width: 180, render: (iso?: string) => iso ? new Date(iso).toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' }) : '-' },
]

function buildCrdtChart(convergence: ConfigCenterState['crdtConvergence']): any {
  const labels = convergence.map((c) => c.shard)
  const versions = convergence.map((c) => c.version)
  const colors = convergence.map((c) => c.converged ? '#16c784' : '#f5a623')
  return { tooltip: { trigger: 'axis' }, grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true }, xAxis: { type: 'category', data: labels, axisLine: { lineStyle: { color: '#aab' } } }, yAxis: { type: 'value', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } }, series: [{ type: 'bar', data: versions.map((v, i) => ({ value: v, itemStyle: { color: colors[i] } })) }] }
}
