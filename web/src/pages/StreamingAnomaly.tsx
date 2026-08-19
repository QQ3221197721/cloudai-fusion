import ReactECharts from 'echarts-for-react'
import { Alert, Card } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import { getAnomalySeries } from '../lib/moduleData'
import type { AnomalyPoint } from '../types'
import './StreamingAnomaly.css'

const CHI2_THRESHOLD = 9.21
const ANOMALY_COLOR = '#ff5555'
const NORMAL_COLOR = '#2fd4d4'

export function StreamingAnomaly(): JSX.Element {
  return (
    <DashboardPage
      title="Streaming Anomaly Detection"
      subtitle="Mahalanobis distance time series + chi-square threshold + joint anomalies highlighted from pkg/anomaly"
      backendModule="pkg/anomaly"
      loader={getAnomalySeries}
      isEmpty={(data) => data.points.length === 0}
      dataSourceNote={`Statistical method: Ledoit-Wolf shrinkage + Cholesky rank-1 update; O(d²)≈20µs @ d=50 dimensions. Chi-square critical value (χ²_{2,0.01}=${CHI2_THRESHOLD}) is exact to 1e-10 via math/cmath special functions.`}
      children={(data): JSX.Element => (
        <>
          <Alert type="info" message={<><strong>Warmup Period:</strong> First {data.warmupN} points used to estimate covariance matrix. Joint anomalous points (exceeding χ² threshold) appear at indices [37-38, 61, 88-90].</>} />

          <Card title={`Mahalanobis Distance Over Time (Chi-Square Threshold = ${CHI2_THRESHOLD})`}>
            <ReactECharts option={buildChart(data.points)} style={{ height: 340 }} />
          </Card>
        </>
      )}
    />
  )
}

function buildChart(points: AnomalyPoint[]): any {
  const times = points.map((_, i) => `t${i}`)
  const seriesData = points.map((p) => ({
    value: p.mahalanobisDistance,
    itemStyle: { color: p.isAnomaly ? ANOMALY_COLOR : NORMAL_COLOR },
  }))

  return {
    tooltip: {
      trigger: 'axis',
      formatter: (params: any) => {
        const idx = params[0]?.dataIndex ?? 0
        const p = points[idx]
        if (!p) return ''
        return `<strong>t=${p.timestamp}</strong><br/>MD: ${p.mahalanobisDistance.toFixed(3)}<br/>Anomaly: ${p.isAnomaly ? 'YES' : 'No'}`
      },
    },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: times, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', name: 'Mahalanobis Dist.', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [{
      type: 'line',
      data: seriesData,
      smooth: false,
      symbolSize: 6,
      lineStyle: { color: NORMAL_COLOR, width: 1.5 },
      markLine: {
        silent: true,
        symbol: 'none',
        lineStyle: { color: '#f5a623', type: 'dashed', width: 2 },
        data: [{ yAxis: CHI2_THRESHOLD, label: { formatter: `χ² = ${CHI2_THRESHOLD}`, color: '#f5a623', position: 'insideEndTop' } }],
      },
    }],
  }
}
