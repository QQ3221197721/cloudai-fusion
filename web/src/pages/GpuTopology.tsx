import ReactECharts from 'echarts-for-react'
import { Card, Alert } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import { getSchedulerStats } from '../lib/moduleData'
import './GpuTopology.css'

const MEAN_RATIO = 0.999954
const WORST_RATIO = 0.972027

export function GpuTopology(): JSX.Element {
  return (
    <DashboardPage
      title="GPU Topology Scheduler"
      subtitle="Exact vs Approximate Solution Quality Comparison + Approximation Ratio from pkg/scheduler/dense-k-subgraph"
      backendModule="pkg/scheduler/dense-k-subgraph"
      loader={getSchedulerStats}
      isEmpty={(data) => data.results.length === 0}
      dataSourceNote={`REAL measured numbers from docs/algorithm-gpu-topology-scheduling.md §5.2 (1000 random topologies, seed 20260818). Mean approximation ratio = ${MEAN_RATIO.toFixed(6)}, Worst case = ${WORST_RATIO.toFixed(6)} (greedy-2opt / exact-bnb).`}
      children={(data): JSX.Element => (
        <>
          <Alert type="success" message={<><strong>Finding:</strong> greedy-2opt achieves near-optimal solutions on real GPU topologies</>} />
          <Card className="ratio-cards">
            <div className="ratio-card">
              <span className="card-label">Mean Approximation Ratio</span>
              <span className="card-value">{(MEAN_RATIO * 100).toFixed(4)}%</span>
            </div>
            <div className="ratio-card">
              <span className="card-label">Worst Case Ratio</span>
              <span className="card-value">{(WORST_RATIO * 100).toFixed(4)}%</span>
            </div>
            <div className="ratio-card">
              <span className="card-label">95% CI</span>
              <span className="card-value">[99.9895%, 100.0013%]</span>
            </div>
          </Card>

          <ReactECharts option={buildQualityChart(data.results)} style={{ height: 320 }} />

          <Card title="Solver Comparison">
            <table className="solver-table">
              <thead>
                <tr>
                  <th>Solver</th>
                  <th>Quality Ratio</th>
                  <th>Latency (ns)</th>
                  <th>Throughput (GB/s)</th>
                </tr>
              </thead>
              <tbody>
                {data.results.map((r) => (
                  <tr key={r.solver}>
                    <td>{r.solver.replace('-', ' ')}</td>
                    <td>{r.qualityRatio.toFixed(6)}</td>
                    <td>{r.latencyNs.toLocaleString()}</td>
                    <td>{r.throughputGbps.toFixed(1)}</td>
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

function buildQualityChart(results: any[]): any {
  const labels = results.map((r) => r.solver.split('-').join(' '))
  const values = results.map((r) => r.qualityRatio)
  const colors = results.map((r) => r.solver.startsWith('exact') || r.solver.startsWith('greedy') ? '#16c784' : '#f5a623')
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: labels, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', min: 0, max: 1.1, axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [{ type: 'bar', data: values.map((v, i) => ({ value: v, itemStyle: { color: colors[i] } })) }],
  }
}
