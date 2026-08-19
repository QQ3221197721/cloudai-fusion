import ReactECharts from 'echarts-for-react'
import { Alert, Row, Col, Statistic, Card } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import { getDriftStats } from '../lib/moduleData'
import './ModelDrift.css'

const PSI_WARN_THRESHOLD = 0.1
const PSI_BREACH_THRESHOLD = 0.25

export function ModelDrift(): JSX.Element {
  return (
    <DashboardPage
      title="Model Drift"
      subtitle="PSI/KS curves + WARN/BREACH threshold lines from pkg/mlops"
      backendModule="pkg/mlops"
      loader={getDriftStats}
      isEmpty={(data) => data.points.length === 0}
      dataSourceNote="Illustrative mock shaped to pkg/mlops contracts."
      children={(data): JSX.Element => (
        <>
          {data.breachedAt && (
            <Alert type="error" message={`Threshold BREACH detected at ${new Date(data.breachedAt).toLocaleString('en-US')}`} showIcon />
          )}

          <Row gutter={[16, 16]} className="stat-row">
            <Col span={8}>
              <div className="stat-box">Max PSI</div>
              <Statistic value={data.maxPsi.toFixed(4)} prefix="" valueStyle={{ color: '#f5a623' }} />
            </Col>
            <Col span={8}>
              <div className="stat-box">Max KS</div>
              <Statistic value={data.maxKs.toFixed(4)} prefix="" />
            </Col>
            <Col span={8}>
              <div className="stat-box">Breached At</div>
              <Statistic value={data.breachedAt ? new Date(data.breachedAt).toLocaleDateString() : '—'} />
            </Col>
          </Row>

          <Card title={`PSI Over Time (Thresholds: WARN=${PSI_WARN_THRESHOLD}, BREACH=${PSI_BREACH_THRESHOLD})`}>
            <ReactECharts option={buildPsiChart(data.points)} style={{ height: 300 }} />
          </Card>

          <Card title="KS Over Time">
            <ReactECharts option={buildKsChart(data.points)} style={{ height: 300 }} />
          </Card>
        </>
      )}
    />
  )
}

function buildPsiChart(points: any[]): any {
  const labels = points.map((p) => new Date(p.timestamp).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }))
  const values = points.map((p) => p.psi)
  return {
    tooltip: { trigger: 'axis' },
    legend: { data: ['PSI'], textStyle: { color: '#aab' } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: labels, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } }, max: () => Math.max(...values, 0.3) },
    series: [{
      name: 'PSI',
      type: 'line',
      data: values,
      smooth: true,
      areaStyle: {},
      itemStyle: { color: '#2fd4d4' },
      markLine: {
        silent: true,
        symbol: 'none',
        lineStyle: { color: '#ff5555', width: 2 },
        data: [
          { yAxis: PSI_WARN_THRESHOLD, label: { formatter: `WARN (${PSI_WARN_THRESHOLD})`, color: '#f5a623', position: 'insideEndTop' } },
          { yAxis: PSI_BREACH_THRESHOLD, label: { formatter: `BREACH (${PSI_BREACH_THRESHOLD})`, color: '#ff5555', position: 'insideEndBottom' } },
        ],
      },
    }],
  }
}

function buildKsChart(points: any[]): any {
  const labels = points.map((p) => new Date(p.timestamp).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }))
  const values = points.map((p) => p.ks)
  return {
    tooltip: { trigger: 'axis' },
    legend: { data: ['KS'], textStyle: { color: '#aab' } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: labels, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [{ name: 'KS', type: 'line', data: values, smooth: true, itemStyle: { color: '#16c784' } }],
  }
}
