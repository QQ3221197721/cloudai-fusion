import ReactECharts from 'echarts-for-react'
import { Card, Alert, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DashboardPage } from '../components/DashboardPage'
import { getQuantileBenchmark } from '../lib/moduleData'
import type { QuantileComparison } from '../types'
import './ExactQuantile.css'

export function ExactQuantile(): JSX.Element {
  return (
    <DashboardPage
      title="Exact Quantile"
      subtitle="Error comparison vs Prometheus / t-digest / KLL + memory footprint from pkg/quantile TailExact"
      backendModule="pkg/quantile"
      loader={getQuantileBenchmark}
      isEmpty={(data) => data.comparisons.length === 0}
      dataSourceNote="REAL measured numbers from docs/algorithm-exact-quantile.md (Normal N(0,1), n=20000). On heavy-tailed distributions, Prometheus bucket interpolation reaches +132.7% (Lognormal) / +182.2% (Pareto) relative error at p999, while TailExact stays exact at the tails."
      children={(data): JSX.Element => (
        <>
          <Alert
            type="success"
            message={<><strong>TailExact(K=500)</strong> achieves zero absolute error at p99/p999 at only 14KB — matching sketch memory but with tail-exact guarantees.</>}
          />

          <Card title={`Absolute Error by Quantile — ${data.dataset}`}>
            <ReactECharts option={buildErrorChart(data.comparisons)} style={{ height: 340 }} />
          </Card>

          <Card title="Memory Footprint">
            <ReactECharts option={buildMemChart(data.comparisons)} style={{ height: 260 }} />
          </Card>

          <Card title="Full Comparison">
            <Table<QuantileComparison> columns={columns} dataSource={data.comparisons} rowKey="estimator" pagination={false} scroll={{ x: 800 }} />
          </Card>
        </>
      )}
    />
  )
}

const columns: ColumnsType<QuantileComparison> = [
  { title: 'Estimator', dataIndex: 'estimator', key: 'estimator', width: 180, render: (v) => <code className="est-code">{v}</code> },
  { title: 'p50 err', key: 'p50', width: 90, render: (_, r) => r.absErr.p50.toFixed(3) },
  { title: 'p90 err', key: 'p90', width: 90, render: (_, r) => r.absErr.p90.toFixed(3) },
  { title: 'p99 err', key: 'p99', width: 90, render: (_, r) => r.absErr.p99.toFixed(3) },
  { title: 'p999 err', key: 'p999', width: 90, render: (_, r) => r.absErr.p999.toFixed(3) },
  { title: 'Memory', dataIndex: 'memoryBytes', key: 'memoryBytes', width: 110, render: (b: number) => `${(b / 1000).toFixed(0)}KB` },
  { title: 'Insert ops/s', dataIndex: 'insertOpsPerSec', key: 'insertOpsPerSec', width: 130, render: (o: number) => `${(o / 1e6).toFixed(1)}M` },
]

function buildErrorChart(comparisons: QuantileComparison[]): any {
  const quantiles = ['p50', 'p90', 'p99', 'p999'] as const
  return {
    tooltip: { trigger: 'axis' },
    legend: { data: comparisons.map((c) => c.estimator), textStyle: { color: '#aab' }, type: 'scroll' },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: quantiles, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', name: 'abs error', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: comparisons.map((c) => ({
      name: c.estimator,
      type: 'line',
      data: quantiles.map((q) => c.absErr[q]),
      smooth: false,
      lineStyle: { width: c.estimator.startsWith('TailExact') ? 3 : 1.5 },
    })),
  }
}

function buildMemChart(comparisons: QuantileComparison[]): any {
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: comparisons.map((c) => c.estimator), axisLabel: { rotate: 20 }, axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', name: 'KB', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [{ type: 'bar', data: comparisons.map((c) => Number((c.memoryBytes / 1000).toFixed(0))), itemStyle: { color: '#2fd4d4' } }],
  }
}
