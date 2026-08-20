import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Spin, Empty, Tag, Space, Alert } from 'antd'
import { ReloadOutlined, ApiOutlined, WarningOutlined } from '@ant-design/icons'
import type { DataEnvelope } from '../types'
import './DashboardPage.css'

interface Props<T> {
  title: string
  subtitle: string
  /** pkg/... backend module this dashboard reflects. */
  backendModule: string
  /** Async loader returning an honest DataEnvelope (api | mock). */
  loader: () => Promise<DataEnvelope<T>>
  /** True if the payload should be treated as empty (renders Empty state). */
  isEmpty?: (data: T) => boolean
  /** Renders the actual dashboard body once data is available. */
  children: (data: T, env: DataEnvelope<T>) => ReactNode
  /** Optional note about the provenance of the numbers shown. */
  dataSourceNote?: ReactNode
}

// DashboardPage is the shared shell for every module dashboard. It owns the
// loading / error / empty tri-state, the honest source badge (API vs MOCK),
// a refresh control, and the loud mock disclosure banner. Individual pages
// only supply a loader + a body renderer, keeping them small and consistent.
export function DashboardPage<T>({
  title,
  subtitle,
  backendModule,
  loader,
  isEmpty,
  children,
  dataSourceNote,
}: Props<T>): JSX.Element {
  const [loading, setLoading] = useState(true)
  const [env, setEnv] = useState<DataEnvelope<T> | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const result = await loader()
      setEnv(result)
    } catch (e: unknown) {
      setError(String(e))
      setEnv(null)
    } finally {
      setLoading(false)
    }
  }, [loader])

  useEffect(() => {
    void load()
  }, [load])

  const isMock = env?.source === 'mock'
  // A backend that WAS reached but honestly reports no real hardware present
  // (mode='simulated'). Distinct from isMock (backend unreachable).
  const isSimulated = env?.source === 'api' && !!env.simulated

  return (
    <div className="dashboard-page">
      <div className="dashboard-header">
        <div className="dashboard-heading">
          <h1 className="dashboard-title">{title}</h1>
          <p className="dashboard-subtitle">{subtitle}</p>
          <code className="dashboard-module">{backendModule}</code>
        </div>
        <Space wrap align="center">
          {env && (
            <Tag
              color={isMock ? 'orange' : isSimulated ? 'gold' : 'green'}
              icon={isMock || isSimulated ? <WarningOutlined /> : <ApiOutlined />}
            >
              {isMock ? '[MOCK DATA]' : isSimulated ? '[SIMULATED - no hardware]' : '[API LIVE]'}
            </Tag>
          )}
          <ReloadOutlined
            className="dashboard-refresh"
            onClick={() => void load()}
            title="Refresh"
            aria-label="Refresh data"
          />
        </Space>
      </div>

      {isMock && env && (
        <Alert
          className="dashboard-mock-banner"
          type="warning"
          showIcon
          banner
          message="This view is showing MOCK data — no live backend response."
          description={env.reason}
        />
      )}

      {isSimulated && env && (
        <Alert
          className="dashboard-mock-banner"
          type="warning"
          showIcon
          banner
          message="[SIMULATED - no hardware] — the backend was reached but reports no real hardware on this host."
          description={env.reason}
        />
      )}

      {dataSourceNote && (
        <Alert className="dashboard-source-note" type="info" showIcon message={dataSourceNote} />
      )}

      {error && (
        <div className="dashboard-error" role="alert">
          Error loading data: {error}
        </div>
      )}

      {loading && !env && (
        <div className="dashboard-loading">
          <Spin size="large" tip="Loading…" />
        </div>
      )}

      {!loading && !error && env && isEmpty?.(env.data) && (
        <Empty description="No data available for this module." style={{ marginTop: 48 }} />
      )}

      {env && !(isEmpty?.(env.data)) && (
        <div className="dashboard-body">{children(env.data, env)}</div>
      )}
    </div>
  )
}
