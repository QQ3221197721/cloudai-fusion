import ReactECharts from 'echarts-for-react'
import { Statistic, Row, Col, Card, Progress, Alert } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import { getEventBusMetrics } from '../lib/moduleData'
import './EventFabric.css'

export function EventFabricThroughput(): JSX.Element {
  return (
    <DashboardPage
      title="Event Fabric Throughput"
      subtitle="events/sec, hop distribution, signature overhead from pkg/eventbus"
      backendModule="pkg/eventbus"
      loader={getEventBusMetrics}
      isEmpty={(data) => data.eventsPerSec === 0}
      dataSourceNote="Illustrative mock shaped to pkg/eventbus contracts. When /api/v1/eventbus/metrics is live, source will change to [API LIVE]."
      children={(data): JSX.Element => (
        <>
          <Row gutter={[16, 16]} className="metric-cards">
            <Col span={8}>
              <div className="metric-card">
                <p className="metric-title">Events/sec</p>
                <Statistic value={Math.round(data.eventsPerSec).toLocaleString()} />
              </div>
            </Col>
            <Col span={8}>
              <div className="metric-card">
                <p className="metric-title">Avg Latency</p>
                <Statistic value={data.avgLatencyMs.toFixed(2)} suffix="ms" />
              </div>
            </Col>
            <Col span={8}>
              <div className="metric-card">
                <p className="metric-title">Consumer Lag</p>
                <Statistic value={data.consumerLag} suffix="msgs" />
              </div>
            </Col>
          </Row>

          <Row gutter={[16, 16]}>
            <Col span={12}>
              <Card title="Hops Distribution">
                <ReactECharts option={buildHopChart(data.hopDistribution)} style={{ height: 280 }} />
              </Card>
            </Col>
            <Col span={12}>
              <Card title="Signature Overhead">
                <Progress percent={Math.round(data.signatureOverheadMs * 100 / 5)} strokeColor="#2fd4d4" showInfo />
                <p className="sig-overhead-value">{data.signatureOverheadMs.toFixed(3)} ms / ~{Math.round(data.signatureOverheadMs * 100 / 5)}% of 5ms budget</p>
                <Alert type="info" message="Ed25519 sign/verify per hop on event delivery" showIcon style={{ marginTop: 12 }} />
              </Card>
            </Col>
          </Row>
        </>
      )}
    />
  )
}

function buildHopChart(hopDist: { hops: number; count: number }[]): any {
  const labels = hopDist.map((h) => `${h.hops} hop${h.hops > 1 ? 's' : ''}`)
  const values = hopDist.map((h) => h.count)
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: labels, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [{ type: 'bar', data: values, barWidth: '40%' } ],
  }
}
