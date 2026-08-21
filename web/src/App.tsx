import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { Header } from './components/Header'
import { Sidebar } from './components/Sidebar'
import { InteractiveTutorial } from './components/InteractiveTutorial'
import { Overview } from './pages/Overview'
import { CapabilitiesPanel } from './pages/Capabilities'
import { GPUHeatmap } from './pages/GPUHeatmap'
import { EvidenceVerify } from './pages/EvidenceVerify'
import { ProviderManagement } from './pages/ProviderManagement'
import { EventFabricThroughput } from './pages/EventFabric'
import { ConfigCenter } from './pages/ConfigCenter'
import { TrainingJobs } from './pages/TrainingJobs'
import { Experiments } from './pages/Experiments'
import { ModelDrift } from './pages/ModelDrift'
import { GpuTopology } from './pages/GpuTopology'
import { ExactQuantile } from './pages/ExactQuantile'
import { StreamingAnomaly } from './pages/StreamingAnomaly'
import { DeltaSync } from './pages/DeltaSync'
import { CausalAlert } from './pages/CausalAlert'
import { ApiClientGen } from './pages/ApiClientGen'
import { DocGen } from './pages/DocGen'
import { GpuMigDashboard } from './pages/GpuMigDashboard'
import { GpuMigrationDashboard } from './pages/GpuMigrationDashboard'
import { SgxEnclaveDashboard } from './pages/SgxEnclaveDashboard'
import SetupWizard from './pages/developer/SetupWizard'
import SandboxRunner from './pages/developer/SandboxRunner'
import { AutoscaleEngine } from './pages/AutoscaleEngine'
import { TopologyScheduler } from './pages/TopologyScheduler'
import type { RunMode } from './types'

const MOCK_RUN_MODE: RunMode = 'simulation'

export function App(): JSX.Element {
  return (
    <BrowserRouter>
      <Header mode={MOCK_RUN_MODE} isMockSource mockReason="Backend unreachable" />
      <div className="app-layout">
        <Sidebar />
        <main className="main-content">
          <Routes>
            <Route path="/" element={<Overview />} />
            <Route path="/capabilities" element={<CapabilitiesPanel />} />
            <Route path="/gpu" element={<GPUHeatmap />} />
            <Route path="/evidence" element={<EvidenceVerify />} />
            <Route path="/providers" element={<ProviderManagement />} />
            <Route path="/eventbus" element={<EventFabricThroughput />} />
            <Route path="/config" element={<ConfigCenter />} />
            <Route path="/training" element={<TrainingJobs />} />
            <Route path="/experiments" element={<Experiments />} />
            <Route path="/drift" element={<ModelDrift />} />
            <Route path="/gpu-topology" element={<GpuTopology />} />
            <Route path="/quantile" element={<ExactQuantile />} />
            <Route path="/anomaly" element={<StreamingAnomaly />} />
            <Route path="/deltasync" element={<DeltaSync />} />
            <Route path="/correlation" element={<CausalAlert />} />
            <Route path="/api-client-gen" element={<ApiClientGen />} />
            <Route path="/doc-gen" element={<DocGen />} />
            <Route path="/gpu-mig" element={<GpuMigDashboard />} />
            <Route path="/gpu-migration" element={<GpuMigrationDashboard />} />
            <Route path="/sgx" element={<SgxEnclaveDashboard />} />
            {/* Developer Experience (M41/M42) */}
            <Route path="/dev-setup" element={<SetupWizard />} />
            <Route path="/sandbox" element={<SandboxRunner />} />
            {/* Auto-scaling Engine (M16) */}
            <Route path="/autoscale" element={<AutoscaleEngine />} />
            {/* GPU Topology Scheduler (M3 T4) */}
            <Route path="/gpu-topology-scheduler" element={<TopologyScheduler />} />
          </Routes>
        </main>
      </div>
      <InteractiveTutorial />
    </BrowserRouter>
  )
}
