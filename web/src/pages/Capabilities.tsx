import { useState, useEffect } from 'react'
import type { ReactNode } from 'react'
import { Table, Tag, Space, Row, Col, Statistic, Card, Spin, Divider } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { getCapabilities } from '../lib/api'
import type { CapabilitiesResponse, CapabilityBackend, DataEnvelope } from '../types'
import type { ColumnsType } from 'antd/es/table'
import './Capabilities.css'

export function CapabilitiesPanel(): JSX.Element {
  const [loading, setLoading] = useState(true)
  const [dataEnv, setDataEnv] = useState<DataEnvelope<CapabilitiesResponse> | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    ;(async () => {
      try {
        setLoading(true)
        setError(null)
        const env = await getCapabilities()
        setDataEnv(env)
      } catch (e: unknown) {
        setError(String(e))
        setDataEnv(null)
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  const refetch = async () => {
    try {
      setLoading(true)
      setError(null)
      const env = await getCapabilities()
      setDataEnv(env)
    } catch (e: unknown) {
      setError(String(e))
    } finally {
      setLoading(false)
    }
  }

  if (loading && !dataEnv) {
    return <div style={{ padding: 40, textAlign: 'center' }}><Spin size="large" tip="Loading capabilities..." /></div>
  }

  return (
    <div style={{ padding: '20px 24px' }}>
      <Row gutter={[16, 16]} align="middle">
        <Col flex="auto">
          <h1 style={{ margin: 0, fontFamily: 'Chakra Petch, sans-serif', fontSize: '28px', fontWeight: 700 }}>Capability Status</h1>
        </Col>
        <Col>
          <Space>
            <Tag color={dataEnv?.source === 'api' ? 'green' : 'orange'}>{dataEnv?.source === 'api' ? 'API Live' : 'Mock Source'}</Tag>
            {dataEnv?.reason && <Tag color="red">{dataEnv.reason.split(' ').slice(0,3).join(' ')}</Tag>}
          </Space>
        </Col>
        <Col>
          <ReloadOutlined onClick={refetch} style={{ cursor: 'pointer', fontSize: 18 }} title="Refresh" />
        </Col>
      </Row>

      <Divider />

      {error && <div className="error-banner" role="alert">Error loading data: {error}</div>}

      {dataEnv && (
        <GridSummary summary={{ mode: dataEnv.data.run_mode, allReal: dataEnv.data.all_real, simulatedCount: dataEnv.data.simulated_count }} source={dataEnv.source} />
      )}

      <Card style={{ marginTop: 16, boxShadow: '0 2px 8px rgba(0,0,0,0.15)' }} bodyStyle={{ padding: 16 }}>
        <h3 style={{ margin: '0 0 12px 0', fontFamily: 'Chakra Petch, sans-serif', fontSize: 18 }}>Subsystem Backends</h3>
        {dataEnv ? <BackendTable backends={dataEnv.data.backends} /> : <p>No backend data available.</p>}
      </Card>
    </div>
  )
}

interface GridSummaryProps {
  summary: { mode: string; allReal: boolean; simulatedCount: number }
  source: 'api' | 'mock'
}

// GridSummary shows a concise run-mode badge + key metrics at card top.
function GridSummary({ summary, source }: GridSummaryProps): JSX.Element {
  const isMock = source === 'mock'
  return (
    <Card style={{ marginBottom: 16, boxShadow: '0 2px 8px rgba(0,0,0,0.15)' }} bodyStyle={{ padding: 16 }}>
      <Row gutter={[16, 16]}>
        <Col span={8}>
          <Statistic
            title="Run Mode"
            value={summary.mode.toUpperCase()}
            valueStyle={{ fontFamily: 'IBM Plex Mono, monospace', fontWeight: 600 }}
          />
        </Col>
        <Col span={8}>
          <Statistic
            title="All Real Backend?"
            value={summary.allReal ? 1 : 0}
            valueStyle={{ fontFamily: 'IBM Plex Mono, monospace', fontWeight: 600, color: summary.allReal ? '#16c784' : '#ff5555' }}
          />
        </Col>
        <Col span={8}>
          <Statistic
            title="Simulated Subsystems"
            value={summary.simulatedCount}
            valueStyle={{ fontFamily: 'IBM Plex Mono, monospace', fontWeight: 600, color: summary.simulatedCount > 0 ? '#f5a623' : undefined }}
          />
        </Col>
      </Row>
      {isMock && (
        <div className="mock-banner" role="note" aria-live="polite">
          Data displayed is MOCK — backend unreachable. /api/v1/capabilities was unavailable during runtime.
        </div>
      )}
    </Card>
  )
}

interface BackendTableProps {
  backends: CapabilityBackend[]
}

// BackendTable renders per-subsystem real vs simulated rows. SIMULATED rows
// have an orange background tint and explicit `[SIMULATED]` tag.
function BackendTable({ backends }: BackendTableProps): JSX.Element {
  const columns: ColumnsType<CapabilityBackend> = [
    {
      title: 'Subsystem',
      dataIndex: 'component',
      key: 'component',
      fixed: 'left',
      width: 220,
      render: (text: string) => <strong style={{ fontFamily: 'Chakra Petch, sans-serif' }}>{text}</strong>,
    },
    {
      title: 'Status',
      dataIndex: 'mode',
      key: 'mode',
      width: 140,
      render: (mode: string): ReactNode => {
        if (mode === 'real') {
          return <Tag color="green">[REAL]</Tag>
        }
        if (mode === 'simulated') {
          return <Tag color="orange">[SIMULATED]</Tag>
        }
        return <Tag color="default">[DISABLED]</Tag>
      },
    },
    { title: 'Driver', dataIndex: 'driver', key: 'driver', width: 140 },
    { title: 'Detail', dataIndex: 'detail', key: 'detail', ellipsis: true },
    {
      title: 'Registered At',
      dataIndex: 'registered_at',
      key: 'registered_at',
      width: 180,
      render: (iso?: string) => iso ? new Date(iso).toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' }) : '–',
    },
  ]

  // Simulated rows get extra background tint via custom rowClassName.
  const rowClassName = (record: CapabilityBackend): string => (record.mode === 'simulated' ? 'row-simulated' : '')

  return <Table<CapabilityBackend> columns={columns} dataSource={backends} rowKey="component" rowClassName={rowClassName} scroll={{ x: 600 }} pagination={false} />
}
