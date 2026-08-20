import { NavLink } from 'react-router-dom'
import { HomeOutlined, ClusterOutlined, DashboardOutlined, SafetyCertificateOutlined, CloudOutlined, ThunderboltOutlined, SettingOutlined, RocketOutlined, AppstoreOutlined, FundOutlined, DesktopOutlined, BarChartOutlined, BugOutlined, SyncOutlined, LinkOutlined, ApiOutlined, FileTextOutlined, PartitionOutlined, SwapOutlined, LockOutlined } from '@ant-design/icons'
import './Sidebar.css'

const NAV = [
  { to: '/', label: 'Overview', icon: <HomeOutlined />, end: true },
  { to: '/capabilities', label: 'Capabilities', icon: <ClusterOutlined /> },
  { to: '/gpu', label: 'GPU Heatmap', icon: <DashboardOutlined /> },
  { to: '/evidence', label: 'Evidence', icon: <SafetyCertificateOutlined /> },
  // New modules (6 verified backend + 5 algorithms)
  { to: '/providers', label: 'Provider Management', icon: <CloudOutlined /> },
  { to: '/eventbus', label: 'Event Fabric', icon: <ThunderboltOutlined /> },
  { to: '/config', label: 'Config Center', icon: <SettingOutlined /> },
  { to: '/training', label: 'Training Jobs', icon: <RocketOutlined /> },
  { to: '/experiments', label: 'Experiments', icon: <AppstoreOutlined /> },
  { to: '/drift', label: 'Model Drift', icon: <FundOutlined /> },
  { to: '/gpu-topology', label: 'GPU Topology', icon: <DesktopOutlined /> },
  { to: '/quantile', label: 'Exact Quantile', icon: <BarChartOutlined /> },
  { to: '/anomaly', label: 'Streaming Anomaly', icon: <BugOutlined /> },
  { to: '/deltasync', label: 'Delta Sync', icon: <SyncOutlined /> },
  { to: '/correlation', label: 'Causal Alert', icon: <LinkOutlined /> },
  // Developer experience generators (real bench-backed)
  { to: '/api-client-gen', label: 'API Client Gen', icon: <ApiOutlined /> },
  { to: '/doc-gen', label: 'Doc Generator', icon: <FileTextOutlined /> },
  // Hardware validation dashboards (M2 / M3 / M5)
  { to: '/gpu-mig', label: 'GPU MIG', icon: <PartitionOutlined /> },
  { to: '/gpu-migration', label: 'GPU Migration', icon: <SwapOutlined /> },
  { to: '/sgx', label: 'SGX Enclaves', icon: <LockOutlined /> },
]

export function Sidebar(): JSX.Element {
  return (
    <aside className="sidebar">
      <nav>
        {NAV.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            className={({ isActive }) => `nav-item${isActive ? ' nav-item-active' : ''}`}
          >
            <span className="nav-icon">{item.icon}</span>
            <span className="nav-label">{item.label}</span>
          </NavLink>
        ))}
      </nav>
      <div className="sidebar-footer">
        <span className="build-tag">MODULES 39–44</span>
        <span className="build-sub">Dashboard UI &amp; DevEx</span>
      </div>
    </aside>
  )
}
