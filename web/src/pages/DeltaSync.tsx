import ReactECharts from 'echarts-for-react'
import { Tag, Card, Alert } from 'antd'
import { DashboardPage } from '../components/DashboardPage'
import { getDeltaSyncBenchmark } from '../lib/moduleData'
import './DeltaSync.css'

export function DeltaSync(): JSX.Element {
  return (
    <DashboardPage
      title="Incremental Sync"
      subtitle="Amplification ratio comparison + dedup rate from pkg/deltasync/FastCDC"
      backendModule="pkg/deltasync"
      loader={getDeltaSyncBenchmark}
      isEmpty={(data) => data.results.length === 0}
      dataSourceNote={`REAL measured numbers from docs/algorithm-cdc-delta-sync.md §2.2 (Head Insert scenario, baseSize=256KB). FastCDC achieves only one chunk re-transmission (~9KB) vs NaiveFixed's full 256KB retransmission due to block boundary shift. ${Math.round(262145 / 9117)}x less retransmission.`}
      children={(data): JSX.Element => (
        <>
          <Alert
            type="success"
            message={<><strong>Finding:</strong> FastCDC achieves <Tag color="green">~{Math.round(262145 / 9117)}x amplification advantage</Tag> over fixed-block methods on insertion-heavy workloads.</>}
          />

          <ReactECharts option={buildChart(data.results)} style={{ height: 320 }} />

          <Card title="Method Comparison">
            <table className="sync-table">
              <thead>
                <tr>
                  <th>Method</th>
                  <th>Amplification Factor</th>
                  <th>Throughput (ms)</th>
                  <th>Dedup Rate</th>
                </tr>
              </thead>
              <tbody>
                {data.results.map((r) => (
                  <tr key={r.method}>
                    <td>{r.method}</td>
                    <td className={r.method === 'FastCDC' ? 'good' : 'bad'}>{r.amplificationFactor.toLocaleString()} bytes</td>
                    <td>{r.throughputMs.toFixed(3)} ms</td>
                    <td className={r.dedupRate >= 0.96 ? 'good' : ''}>{(r.dedupRate * 100).toFixed(1)}%</td>
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
  const labels = results.map((r) => r.method.split('-').join(' '))
  const data = results.map((r) => ({
    value: r.amplificationFactor,
    itemStyle: { color: r.method === 'FastCDC' ? '#16c784' : '#ff5555' },
  }))

  return {
    tooltip: { trigger: 'axis', formatter: '{b}: {c} bytes' },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: labels, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', name: 'Retransmitted bytes', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [{ type: 'bar', barWidth: '40%', data }],
  }
}
