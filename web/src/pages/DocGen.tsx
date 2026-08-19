import { Card, Statistic, Row, Col, Table, Tag } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import ReactECharts from 'echarts-for-react'
import { DashboardPage } from '../components/DashboardPage'
import { getDocGenBenchmark } from '../lib/moduleData'
import type { DocGenBenchmark, DocGenBenchRow } from '../types'
import './DocGen.css'

const CATEGORY_COLORS: Record<string, string> = {
  parse: '#16a7e9',
  generate: '#fadb14',
  fullcycle: '#eb2f96',
}

export function DocGen(): JSX.Element {
  return (
    <DashboardPage
      title="Documentation Generator"
      subtitle="Go AST -> Markdown API reference (real bench ns/op + symbol counts)"
      backendModule="pkg/docgen"
      loader={getDocGenBenchmark}
      isEmpty={(data) => data.rows.length === 0}
      dataSourceNote="REAL measured numbers from `go test ./pkg/docgen/ -bench=. -benchmem` (Intel Core Ultra 9 275HX). Medium package: pkg/scheduler with 463 symbols."
      children={(data): JSX.Element => (
        <>
          {/* Stats Header */}
          <Row gutter={[16, 16]} className="dashboard-stats">
            <Col span={8}>
              <Statistic
                title="Symbol Density Range"
                value={`${minSymbols(data).toLocaleString()}–${maxSymbols(data).toLocaleString()}`}
                valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600 }}
                suffix="symbols"
              />
            </Col>
            <Col span={8}>
              <Statistic
                title="Parse Large Package"
                value={parseLargeTime(data)}
                precision={1}
                valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600, color: '#16a7e9' }}
                suffix="s/op"
              />
            </Col>
            <Col span={8}>
              <Statistic
                title="Full Cycle Overhead"
                value={fullCycleOverhead(data)}
                precision={2}
                valueStyle={{ fontFamily: 'IBM Plex Mono', fontWeight: 600, color: '#eb2f96' }}
                suffix="% extra time"
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
                <Table<DocGenBenchRow>
                  columns={columns}
                  dataSource={data.rows}
                  rowKey={(r) => `${r.stage}-${r.category}`}
                  pagination={false}
                  scroll={{ x: 900 }}
                  size="small"
                  className="bench-table"
                />
              </div>
            </div>
          </Card>

          <Card title="Scalability Analysis">
            <div style={{ display: 'flex', gap: 24 }}>
              <div style={{ flex: 1 }}>
                <ReactECharts option={symbolVsTimeChart(data)} style={{ height: 240 }} />
              </div>
              <div style={{ flex: 1 }}>
                <ReactECharts option={categoryComparisonChart(data)} style={{ height: 240 }} />
              </div>
            </div>
          </Card>

          <Card title="Insights">
            <div style={{ lineHeight: 1.8 }}>
              <div><strong>Parse complexity:</strong> ParseDir_Medium runs at {((data.rows.find((r) => r.stage === 'ParseDir_Medium')!.nsPerOp / 1_000_000)).toFixed(2)}s per op on 463 symbols—roughly {formatNSPerSymbol(data)}. This is expected since Go AST walking is linear in the number of declarations.</div>
              <div><strong>Generation cost:</strong> GenerateDoc_Large takes {((data.rows.find((r) => r.stage === 'GenerateDoc_Large')!.nsPerOp / 1_000_000)).toFixed(2)}s for 1920 symbols; text rendering and formatting dominate after the AST is constructed.</div>
              <div><strong>Full cycle:</strong> The FullCycle benchmark (160 symbols) provides a baseline for end-to-end throughput when both parsing and generation are combined.</div>
            </div>
          </Card>
        </>
      )}
    />
  )
}

function minSymbols(bench: DocGenBenchmark): number {
  return Math.min(...bench.rows.map((r) => r.symbols))
}

function maxSymbols(bench: DocGenBenchmark): number {
  return Math.max(...bench.rows.map((r) => r.symbols))
}

function parseLargeTime(bench: DocGenBenchmark): number {
  const medium = bench.rows.find((r) => r.stage === 'ParseDir_Medium')
  return medium ? medium.nsPerOp / 1_000_000 : 0
}

function fullCycleOverhead(bench: DocGenBenchmark): number {
  const smallGen = bench.rows.find((r) => r.stage === 'GenerateDoc_Small')?.nsPerOp || 0
  const fullCycle = bench.rows.find((r) => r.stage === 'FullCycle')?.nsPerOp || 0
  if (smallGen === 0) return 0
  return ((fullCycle - smallGen) / smallGen) * 100
}

function formatNSPerSymbol(bench: DocGenBenchmark): string {
  const medium = bench.rows.find((r) => r.stage === 'ParseDir_Medium')
  if (!medium || medium.symbols === 0) return '?'
  const nsPerSym = medium.nsPerOp / medium.symbols
  if (nsPerSym < 1000) return `${(nsPerSym * 1000).toFixed(0)}ns/symbol`
  return `${(nsPerSym / 1000).toFixed(2)}µs/symbol`
}

const columns: ColumnsType<DocGenBenchRow> = [
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
      { text: 'Generate', value: 'generate' },
      { text: 'Full Cycle', value: 'fullcycle' },
    ],
    onFilter: (value, record) => value === record.category,
    render: (cat) => <Tag color={CATEGORY_COLORS[cat]} style={{ fontWeight: 600 }}>{cat.toUpperCase()}</Tag>,
  },
  {
    title: 'Symbols',
    dataIndex: 'symbols',
    key: 'symbols',
    width: 100,
    sorter: (a, b) => a.symbols - b.symbols,
    render: (n) => <span className='val-num'>{n.toLocaleString()}</span>,
  },
  {
    title: 'ns/op',
    dataIndex: 'nsPerOp',
    key: 'nsPerOp',
    width: 140,
    sorter: (a, b) => a.nsPerOp - b.nsPerOp,
    render: (n) => <span className='val-num'>{formatNs(n)}</span>,
  },
  {
    title: 'B/op',
    dataIndex: 'bytesPerOp',
    key: 'bytesPerOp',
    width: 120,
    sorter: (a, b) => a.bytesPerOp - b.bytesPerOp,
    render: (b) => <span className='val-num'>{formatBytes(b)}</span>,
  },
  {
    title: 'allocs/op',
    dataIndex: 'allocsPerOp',
    key: 'allocsPerOp',
    width: 120,
    sorter: (a, b) => a.allocsPerOp - b.allocsPerOp,
    render: (a) => <span className='val-num'>+{a.toLocaleString()}</span>,
  },
]

// Benchmark stage comparison bar chart
function buildStageChart(rows: DocGenBenchRow[]) {
  return {
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
    legend: { textStyle: { color: '#aab' }, type: 'scroll' },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'value', name: 'ns/op', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    yAxis: { type: 'category', data: rows.map((r) => r.stage), axisLine: { lineStyle: { color: '#aab' } } },
    series: [
      {
        name: 'Performance',
        type: 'bar',
        data: rows.map((r) => r.nsPerOp),
        itemStyle: {
          color: (params: any) => {
            const rowIndex = rows.findIndex((r) => r.stage === params.name)
            const cat = rows[rowIndex]?.category || 'parse'
            return CATEGORY_COLORS[cat] || '#aab'
          },
          borderRadius: [0, 4, 4, 0],
        },
        label: { show: true, position: 'right', formatter: '{c}', fontSize: 10, color: '#aab' },
      },
    ],
  }
}

// Symbol count vs performance (scatter plot)
function symbolVsTimeChart(bench: DocGenBenchmark) {
  return {
    tooltip: {
      trigger: 'item',
      formatter: (params: any) => {
        const idx = bench.rows.findIndex((r) => r.stage === params.name)
        const r = bench.rows[idx]!
        return `<strong>${r.stage}</strong><br/>Symbols: ${r.symbols.toLocaleString()}<br/>ns/op: ${formatNs(r.nsPerOp)}`
      },
    },
    grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
    xAxis: { type: 'value', name: 'symbols', splitNumber: 5, axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    yAxis: { type: 'value', name: 'ns/op', axisLine: { lineStyle: { color: '#aab' } }, splitLine: { lineStyle: { color: '#1f293a' } } },
    series: [
      {
        type: 'scatter',
        symbolSize: (value: any[]) => 8 + (value[1] - minSymbols(bench)) * 0.001,
        data: bench.rows.map((r) => [r.symbols, r.nsPerOp]),
        itemStyle: {
          color: (params: any) => {
            const idx = bench.rows.findIndex((r) => r.stage === params.name)
            const cat = bench.rows[idx]?.category || 'parse'
            return CATEGORY_COLORS[cat] || '#aab'
          },
          shadowBlur: 10,
          shadowColor: 'rgba(255,255,255,0.1)',
        },
        label: {
          show: true,
          formatter: (params: any) => params.name,
          position: 'top',
          color: '#aab',
        },
      },
    ],
  }
}

// Category breakdown pie chart
function categoryComparisonChart(bench: DocGenBenchmark) {
  return {
    tooltip: {
      trigger: 'item',
      formatter: (params: any) => `${params.name}: ${params.percent}% (${formatNs(params.value)})`,
    },
    legend: { top: 'bottom', textStyle: { color: '#aab' }, type: 'scroll' },
    grid: { left: '3%', right: '3%' },
    series: [
      {
        name: 'Total Time',
        type: 'pie',
        radius: ['40%', '70%'],
        avoidLabelOverlap: false,
        itemStyle: {
          borderRadius: 10,
          borderColor: '#0d1117',
          borderWidth: 2,
        },
        label: { show: false },
        data: bench.rows
          .map((r) => ({
            name: `${r.stage} (${r.category})`,
            value: r.nsPerOp,
            itemStyle: { color: CATEGORY_COLORS[r.category] || '#aab' },
          }))
          .sort((a, b) => b.value - a.value),
        emphasis: {
          itemStyle: {
            shadowBlur: 10,
            shadowOffsetX: 0,
            shadowColor: 'rgba(0, 0, 0, 0.5)',
          },
        },
      },
    ],
  }
}

function formatNs(ns: number): string {
  if (ns >= 1_000_000_000) return `${(ns / 1_000_000_000).toFixed(2)}G`
  if (ns >= 1_000_000) return `${(ns / 1_000_000).toFixed(2)}M`
  if (ns >= 1000) return `${(ns / 1000).toFixed(1)}k`
  return `${Math.round(ns)}`
}

function formatBytes(bytes: number): string {
  if (bytes >= 1_000_000) return `${(bytes / 1_000_000).toFixed(1)}M`
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)}KB`
  return `${bytes}`
}
