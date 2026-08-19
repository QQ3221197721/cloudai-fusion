import { Card, Statistic, Row, Col, Table, Tag } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ReactECharts from 'echarts-for-react'
import { DashboardPage } from '../components/DashboardPage'
import { getApiClientGenBenchmark } from '../lib/moduleData'
import type { ClientGenBenchRow, ApiClientGenBenchmark } from '../types'
import './ApiClientGen.css'

const CATEGORY_COLORS: Record<string, string> = {
  parse: '#16a7e9',
  model: '#52c41a',
  generate: '#fadb14',
  fullcycle: '#eb2f96',
}

export function ApiClientGen(): JSX.Element {
  return (
    <DashboardPage
      title="API Client Generator"
      subtitle="OpenAPI/Swagger -> idiomatic Go / TypeScript / Python HTTP clients (real bench ns/op)"
      backendModule="pkg/apiclientgen"
      loader={getApiClientGenBenchmark}
      isEmpty={(data) => data.rows.length === 0}
      dataSourceNote="REAL measured numbers from `go test ./pkg/apiclientgen/ -bench=. -benchmem` (Intel Core Ultra 9 275HX). Supported targets: go/typescript/python."
      children={(data): JSX.Element => (
        <>
          {/* Stats Header */}
          <Row gutter={[16, 16]} className="dashboard-stats">
            <Col span={8}>
              <Statistic
                title="Languages Supported"
                value={data.languages.length}
                valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600 }}
                suffix={`/ ${data.languages.join(', ')}`}
              />
            </Col>
            <Col span={8}>
              <Statistic
                title="Fastest Parse Stage"
                value={fastestParse(data)}
                precision={1}
                valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600, color: '#52c41a' }}
                suffix="µs/op"
              />
            </Col>
            <Col span={8}>
              <Statistic
                title="Slowest Gen Stage"
                value={slowestGen(data)}
                precision={0}
                valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600, color: '#f5a623' }}
                suffix="ms/op"
              />
            </Col>
          </Row>

          <Card
            title={`Performance Benchmarks — CPU: ${data.cpu.split('·')[1].trim()}`}
            bodyStyle={{ padding: 0 }}
          >
            <div style={{ display: 'flex', minHeight: 320 }}>
              <div style={{ flex: 1, paddingRight: 16 }}>
                <ReactECharts option={buildStageChart(data.rows)} style={{ height: 320 }} />
              </div>
              <div style={{ width: 400, overflowY: 'auto', paddingLeft: 16 }}>
                <Table<ClientGenBenchRow>
                  columns={columns}
                  dataSource={data.rows}
                  rowKey={(r) => `${r.stage}-${r.category}-${r.target || ''}`}
                  pagination={false}
                  scroll={{ x: 900 }}
                  size="small"
                  className="bench-table"
                />
              </div>
            </div>
          </Card>

          <Card title="Pipeline Breakdown">
            <div style={{ display: 'flex', gap: 24 }}>
              <div style={{ flex: 1 }}>
                <ReactECharts option={parseVsGenerateChart(data)} style={{ height: 240 }} />
              </div>
              <div style={{ flex: 1 }}>
                <ReactECharts option={memoryVsAllocChart(data)} style={{ height: 240 }} />
              </div>
            </div>
          </Card>
        </>
      )}
    />
  )
}

function fastestParse(bench: ApiClientGenBenchmark): number {
  const parseRows = bench.rows.filter((r) => r.category === 'parse')
  if (!parseRows.length) return 0
  return Math.min(...parseRows.map((r) => r.nsPerOp)) / 1000
}

function slowestGen(bench: ApiClientGenBenchmark): number {
  const genRows = bench.rows.filter((r) => r.category === 'generate')
  if (!genRows.length) return 0
  return Math.max(...genRows.map((r) => r.nsPerOp)) / 1_000_000
}

const columns: ColumnsType<ClientGenBenchRow> = [
  {
    title: 'Stage',
    dataIndex: 'stage',
    key: 'stage',
    fixed: 'left',
    width: 140,
    sorter: (a, b) => a.stage.localeCompare(b.stage),
    render: (text) => <strong>{text}</strong>,
  },
  {
    title: 'Category',
    dataIndex: 'category',
    key: 'category',
    width: 100,
    filters: [
      { text: 'Parse', value: 'parse' },
      { text: 'Model', value: 'model' },
      { text: 'Generate', value: 'generate' },
      { text: 'Full Cycle', value: 'fullcycle' },
    ],
    onFilter: (value, record) => value === record.category,
    render: (cat) => (
      <Tag color={CATEGORY_COLORS[cat]} style={{ fontWeight: 600 }}>{cat.toUpperCase()}</Tag>
    ),
  },
  {
    title: 'Target',
    dataIndex: 'target',
    key: 'target',
    width: 120,
    render: (v) => (v ? <code>{v.toUpperCase()}</code> : '-'),
  },
  {
    title: 'ns/op',
    dataIndex: 'nsPerOp',
    key: 'nsPerOp',
    width: 140,
    sorter: (a, b) => a.nsPerOp - b.nsPerOp,
    render: (n) => <span className='val-num'>{(n / 1000).toFixed(1)}k</span>,
  },
  {
    title: 'B/op',
    dataIndex: 'bytesPerOp',
    key: 'bytesPerOp',
    width: 120,
    sorter: (a, b) => a.bytesPerOp - b.bytesPerOp,
    render: (b) => <span className='val-num'>{(b / 1024).toFixed(0)}KB</span>,
  },
  {
    title: 'allocs/op',
    dataIndex: 'allocsPerOp',
    key: 'allocsPerOp',
    width: 120,
    sorter: (a, b) => a.allocsPerOp - b.allocsPerOp,
    render: (a) => <span className='val-num'>+{a}</span>,
  },
  {
    title: 'Description',
    dataIndex: 'note',
    key: 'note',
    ellipsis: true,
    render: (text) => <span className='note-text'>{text}</span>,
  },
]

// Benchmark stage comparison bar chart
function buildStageChart(rows: ClientGenBenchRow[]) {
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { textStyle: { color: '#aab' }, type: 'scroll' },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: {
      type: 'value',
      name: 'ns/op',
      axisLine: { lineStyle: { color: '#aab' } },
      splitLine: { lineStyle: { color: '#1f293a' } },
    },
    yAxis: { type: 'category', data: rows.map((r) => r.stage), axisLine: { lineStyle: { color: '#aab' } } },
    series: [
      {
        name: 'Performance',
        type: 'bar',
        data: rows.map((r) => r.nsPerOp),
        itemStyle: {
          color: (params: any) => {
            const catMap: Record<string, string> = { parse: '#16a7e9', model: '#52c41a', generate: '#fadb14', fullcycle: '#eb2f96' }
            const rowIndex = rows.findIndex((r) => r.stage === params.name)
            return catMap[rows[rowIndex]?.category || 'parse']
          },
          borderRadius: [0, 4, 4, 0],
        },
        label: { show: true, position: 'right', formatter: '{c} ns/op', fontSize: 10, color: '#aab' },
      },
    ],
  }
}

// Parse vs Generate phase comparison
function parseVsGenerateChart(bench: ApiClientGenBenchmark) {
  const avgNS = (cats: string[]) => {
    const filtered = bench.rows.filter((r) => cats.includes(r.category))
    return filtered.reduce((sum, r) => sum + r.nsPerOp, 0) / (filtered.length || 1)
  }
  const parseAvg = avgNS(['parse', 'model'])
  const genAvg = avgNS(['generate'])
  const cycleAvg = bench.rows.find((r) => r.category === 'fullcycle')?.nsPerOp || 0

  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    grid: { left: '3%', right: '3%', bottom: '3%', containLabel: true },
    xAxis: { type: 'category', data: ['Parse + Model', 'Generate', 'Full Cycle'], axisLine: { lineStyle: { color: '#aab' } } },
    yAxis: { type: 'value', name: 'ns/op', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [
      { type: 'bar', name: 'Parse + Model', data: [parseAvg], itemStyle: { color: '#52c41a' } },
      { type: 'bar', name: 'Generate', data: [genAvg], itemStyle: { color: '#fadb14' } },
      { type: 'bar', name: 'Full Cycle', data: [cycleAvg], itemStyle: { color: '#eb2f96' } },
    ],
  }
}

// Memory footprint vs allocation count
function memoryVsAllocChart(bench: ApiClientGenBenchmark) {
  return {
    tooltip: { trigger: 'item', formatter: '{b}: {c} allocs/op' },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'value', name: 'allocs/op', splitNumber: 5, axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    yAxis: { type: 'category', data: bench.rows.map((r) => r.stage), axisLine: { lineStyle: { color: '#aab' } } },
    series: [
      {
        type: 'scatter',
        symbolSize: (value: any[]) => Math.sqrt(value[1]) * 20,
        data: bench.rows.map((r) => [r.allocsPerOp, r.bytesPerOp]),
        itemStyle: {
          color: (params: any) => {
            const rowIndex = bench.rows.findIndex((r) => r.stage === params.name)
            const cat = bench.rows[rowIndex]?.category || 'parse'
            return cat === 'parse' ? '#16a7e9' : cat === 'generate' ? '#fadb14' : cat === 'fullcycle' ? '#eb2f96' : '#52c41a'
          },
          shadowBlur: 10,
          shadowColor: 'rgba(255,255,255,0.1)',
        },
        label: { show: false },
      },
    ],
  }
}