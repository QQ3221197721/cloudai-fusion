import { useMemo, useRef } from 'react'
import ReactECharts from 'echarts-for-react'
import { Card, Tag, Row, Col, Alert } from 'antd'
import { WarningOutlined } from '@ant-design/icons'
import './GPUHeatmap.css'

const NODE_COUNT = 10
const GPUS_PER_NODE = 8

interface GpuCellValue {
  nodeIndex: number
  gpuIndex: number
  nodeLabel: string
  gpuLabel: string
  utilization: number // 0-100
}

// Deterministic pseudo-random so the ECharts canvas and the screen-reader table
// always render the exact same numbers (no per-render drift).
function deterministicUtil(node: number, gpu: number): number {
  const s = Math.sin(node * 12.9898 + gpu * 78.233) * 43758.5453
  const frac = s - Math.floor(s)
  return Math.round(frac * 100)
}

// GPU utilization heatmap — MOCK because there's no backend endpoint yet.
// We render a 10-node × 8-GPU grid (80 cells). Each cell shows utilization %
// visually (in-cell label) AND exposes it to assistive tech via aria + a
// screen-reader-only semantic <table>.
export function GPUHeatmap(): JSX.Element {
  const chartRef = useRef<ReactECharts | null>(null)

  const cells = useMemo<GpuCellValue[]>(() => {
    const out: GpuCellValue[] = []
    for (let n = 0; n < NODE_COUNT; n++) {
      for (let g = 0; g < GPUS_PER_NODE; g++) {
        out.push({
          nodeIndex: n,
          gpuIndex: g,
          nodeLabel: `Node-${n}`,
          gpuLabel: `GPU-${g}`,
          utilization: deterministicUtil(n, g),
        })
      }
    }
    return out
  }, [])

  const chartOption = useMemo(() => buildHeatmapOption(cells), [cells])

  return (
    <div style={{ padding: '20px 24px' }}>
      <Row gutter={[16, 16]} align="middle">
        <Col flex="auto">
          <h1 style={{ margin: 0, fontFamily: 'Chakra Petch, sans-serif', fontSize: '28px', fontWeight: 700 }}>GPU Utilization</h1>
        </Col>
        <Col>
          <Tag color="orange" icon={<WarningOutlined />}>[MOCK DATA]</Tag>
        </Col>
      </Row>

      <Alert
        message="This view is MOCK — no GPU endpoint available at /api/v1/gpu/topology yet."
        description="Data shown below is synthesized for UI validation only. No real nodes or GPUs are queried."
        type="warning"
        showIcon
        banner
        style={{ marginTop: 12 }}
      />

      <Card className="gpu-card" style={{ marginTop: 16, boxShadow: '0 2px 8px rgba(0,0,0,0.15)' }} bodyStyle={{ padding: 16 }}>
        <div className="chart-container">
          <ReactECharts ref={chartRef} option={chartOption} style={{ height: 620 }} />
        </div>

        {/* Screen-reader-only equivalent of the canvas heatmap. Sighted users see
            the ECharts grid; assistive tech reads this semantic table instead of
            an opaque single Canvas node. */}
        <table className="sr-only" aria-label="GPU utilization heatmap, 10 nodes × 8 GPUs">
          <caption>GPU utilization percentage per node and GPU (10 nodes × 8 GPUs)</caption>
          <thead>
            <tr>
              <th scope="col">Node</th>
              {Array.from({ length: GPUS_PER_NODE }, (_, g) => (
                <th key={g} scope="col">{`GPU-${g}`}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {Array.from({ length: NODE_COUNT }, (_, n) => (
              <tr key={n}>
                <th scope="row">{`Node-${n}`}</th>
                {Array.from({ length: GPUS_PER_NODE }, (_, g) => {
                  const cell = cells[n * GPUS_PER_NODE + g]
                  return (
                    <td key={g}>{`Node-${n} GPU-${g}: ${cell.utilization}%`}</td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </div>
  )
}

function buildHeatmapOption(cells: GpuCellValue[]): Record<string, unknown> {
  const gpuLabels = Array.from({ length: GPUS_PER_NODE }, (_, g) => `GPU-${g}`)
  const nodeLabels = Array.from({ length: NODE_COUNT }, (_, n) => `Node-${n}`)
  // heatmap data points are [xIndex, yIndex, value].
  const data = cells.map((c) => [c.gpuIndex, c.nodeIndex, c.utilization])

  return {
    // Accessibility: expose a text description + decal patterns so the chart is
    // not communicated by color alone.
    aria: {
      enabled: true,
      decal: { show: true },
      label: { description: 'GPU utilization heatmap, 10 nodes × 8 GPUs' },
    },
    title: { text: 'GPU Utilization (%)', left: 'center', textStyle: { fontFamily: 'Chakra Petch, sans-serif', fontSize: 16, color: '#e6edf3' } },
    tooltip: {
      position: 'top',
      formatter: (params: unknown): string => {
        const p = params as { data?: [number, number, number] }
        if (!p || !p.data) return ''
        const [g, n, v] = p.data
        return `<strong>Node-${n} / GPU-${g}</strong><br/>Utilization: <strong>${v}%</strong>`
      },
    },
    grid: { top: 60, left: 80, right: 30, bottom: 40 },
    xAxis: {
      type: 'category',
      data: gpuLabels,
      splitArea: { show: true },
      axisLabel: { color: '#aab' },
    },
    yAxis: {
      type: 'category',
      data: nodeLabels,
      splitArea: { show: true },
      axisLabel: { color: '#aab' },
    },
    visualMap: {
      min: 0,
      max: 100,
      calculable: true,
      orient: 'vertical',
      right: 0,
      top: 'center',
      inRange: { color: ['#16c784', '#2fd4d4', '#f5a623', '#ff5555'] },
      textStyle: { color: '#aab' },
      formatter: (value: number): string => `${Math.round(value)}%`,
    },
    series: [
      {
        name: 'Utilization (%)',
        type: 'heatmap',
        data,
        // Show the actual percentage inside every cell so numbers are visible,
        // not just color-coded.
        label: {
          show: true,
          formatter: (params: unknown): string => {
            const p = params as { data?: [number, number, number] }
            return p && p.data ? `${p.data[2]}%` : ''
          },
          fontFamily: 'IBM Plex Mono, monospace',
          fontSize: 10,
          color: '#0b1113',
        },
        emphasis: { itemStyle: { shadowBlur: 8, shadowColor: 'rgba(0,0,0,0.5)' } },
      },
    ],
  }
}
