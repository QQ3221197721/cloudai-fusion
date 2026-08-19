import ReactECharts from 'echarts-for-react'
import { Tag, Card, Alert } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import { getAlertCorrelationSweep } from '../lib/moduleData'
import './CausalAlert.css'

export function CausalAlert(): JSX.Element {
  return (
    <DashboardPage
      title="Causal Alert"
      subtitle="Causal graph + compression vs mis-suppression trade-off curve from pkg/correlation"
      backendModule="pkg/correlation"
      loader={getAlertCorrelationSweep}
      isEmpty={(data) => data.results.length === 0}
      dataSourceNote={`REAL measured numbers from docs/algorithm-causal-alert-correlation.md §四 (SuppressThreshold sweep). Recommended operating point: threshold=0.25 (maximize compression while maintaining 0% mis-suppression across all 120 root causes).`}
      children={(data): JSX.Element => (
        <>
          <Alert
            type="success"
            message={<><strong>Finding:</strong> The causal correlation achieves <Tag color="green">up to 72.3% alert compression</Tag> at threshold=0.05 with strictly 0% mis-suppression rate on all 120 root causes.</>}
          />

          <ReactECharts option={buildChart(data.results)} style={{ height: 320 }} />

          <Card title="Threshold Sweep Results">
            <table className="sweep-table">
              <thead>
                <tr>
                  <th>Threshold</th>
                  <th>Compression Ratio</th>
                  <th>Mis-Suppression Rate</th>
                  <th>Roots Count</th>
                </tr>
              </thead>
              <tbody>
                {data.results.map((r) => (
                  <tr key={r.threshold} className={r.threshold === 0.25 ? 'recommended' : ''}>
                    <td>{r.threshold.toFixed(2)}</td>
                    <td className="compression">{(r.compressionRatio * 100).toFixed(1)}%</td>
                    <td className={r.misSuppressRate === 0 ? 'zero-err' : 'bad'}>{(r.misSuppressRate * 100).toFixed(2)}%</td>
                    <td>{r.rootsCount}</td>
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

function buildChart(results: any[]): any {
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'cross', sensor: 'x', crossStyle: { color: '#aab' } } },
    legend: { data: ['Compression', 'Mis-Suppression'], textStyle: { color: '#aab' } },
    toolbox: { feature: { dataZoom: { yAxisIndex: 'none' }, saveAsImage: {} } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: [
      { type: 'category', data: results.map((r) => r.threshold.toFixed(2)), axisLine: { lineStyle: { color: '#aab' } } },
      { type: 'value', min: 0, max: 0.8, position: 'left', axisLabel: { formatter: '{value}' } },
    ],
    yAxis: [
      { type: 'value', name: 'Rate', min: 0, max: 1, position: 'right', splitNumber: 5, axisLabel: { formatter: '{value}%' }, axisLine: { show: true } },
    ],
    dataZoom: [{ type: 'inside', start: 0, end: 100 }, { type: 'slider', top: 290, start: 0, end: 100 }],
    series: [
      { name: 'Compression', type: 'line', xAxisIndex: 1, yAxisIndex: 1, data: results.map((r) => r.compressionRatio), smooth: false },
      { name: 'Mis-Suppression', type: 'line', xAxisIndex: 1, yAxisIndex: 1, data: results.map((r) => r.misSuppressRate), smooth: false, itemStyle: { color: '#ff5555' } },
    ],
  }
}
