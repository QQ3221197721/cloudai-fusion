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
          </Routes>
        </main>
      </div>
      <InteractiveTutorial />
    </BrowserRouter>
  )
}
