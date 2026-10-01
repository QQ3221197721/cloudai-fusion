import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useAuthStore } from "@/stores/auth-store";
import { LoginPage } from "@/pages/Login";
import { DashboardPage } from "@/pages/Dashboard";
import { WorkOrderSubmitPage } from "@/pages/WorkOrderSubmit";
import { CampaignsPage } from "@/pages/Campaigns";
import { ReportsPage } from "@/pages/Reports";
import { M7RaftConsensusPage } from "@/pages/M7_RaftConsensus";
import { M8ConfigManagerPage } from "@/pages/M8_ConfigManager";
import { M9GPUSchedulerPage } from "@/pages/M9_GPUScheduler";
import { M10RLOptimizerPage } from "@/pages/M10_RLOptimizer";
import { M11MultiTenantGPUSharingPage } from "@/pages/M11_MultiTenantGPUSharing";
import { M1DistributedLedgerPage } from "@/pages/M1_DistributedLedger";
import { M3FabricConnectivityPage } from "@/pages/M3_FabricConnectivity";
import { M4SecurityBaselinePage } from "@/pages/M4_SecurityBaseline";
import M2ModelLifecyclePage from "@/pages/M2_ModelLifecycle";
import M6FeatureStorePage from "@/pages/M6_FeatureStore";
import { M17AutoMLPlatformPage } from "@/pages/M17_AutoMLPlatform";
import { M18ExperimentTrackerPage } from "@/pages/M18_ExperimentTracker";
import { M19BehaviorHuntingPage } from "@/pages/M19_BehaviorHunting";
import { M20FederatedLearningPage } from "@/pages/M20_FederatedLearning";
import { M36AIVulnerabilityManagementPage } from "@/pages/M36_AIVulnerabilityManagement";
import { M37DevSecOpsPipelinePage } from "@/pages/M37_DevSecOpsPipeline";
import { M38ContainerSecurityPlatformPage } from "@/pages/M38_ContainerSecurityPlatform";
import { M39IdentityAccessManagementPage } from "@/pages/M39_IdentityAccessManagement";
import { M40APISecurityGatewayPage } from "@/pages/M40_APISecurityGateway";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 5, // 5 minutes
      refetchOnWindowFocus: false,
    },
  },
});

// Protected Route Component
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }
  
  return <>{children}</>;
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          {/* Public Routes */}
          <Route path="/login" element={<LoginPage />} />
          
          {/* Protected Routes */}
          <Route
            path="/dashboard"
            element={
              <ProtectedRoute>
                <DashboardPage />
              </ProtectedRoute>
            }
          />
          
          <Route
            path="/workorder-submit"
            element={
              <ProtectedRoute>
                <WorkOrderSubmitPage />
              </ProtectedRoute>
            }
          />
          
          <Route
            path="/quickscan"
            element={
              <ProtectedRoute>
                <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 flex items-center justify-center">
                  <div className="text-center space-y-4 animate-in fade-in slide-in-from-bottom-4 duration-700">
                    <h1 className="text-3xl font-bold gradient-text">Quick Scan</h1>
                    <p className="text-gray-400">Coming soon - vulnerability scanning module</p>
                  </div>
                </div>
              </ProtectedRoute>
            }
          />
          
          <Route
            path="/campaigns"
            element={
              <ProtectedRoute>
                <CampaignsPage />
              </ProtectedRoute>
            }
          />
          
          <Route
            path="/reports"
            element={
              <ProtectedRoute>
                <ReportsPage />
              </ProtectedRoute>
            }
          />
          
          {/* M7 Raft Consensus Module */}
          <Route
            path="/m7-raft-consensus"
            element={
              <ProtectedRoute>
                <M7RaftConsensusPage />
              </ProtectedRoute>
            }
          />
          
          {/* M8 Global Configuration Manager */}
          <Route
            path="/m8-config-manager"
            element={
              <ProtectedRoute>
                <M8ConfigManagerPage />
              </ProtectedRoute>
            }
          />
          
          {/* M9 GPU Scheduler & Live Migration */}
          <Route
            path="/m9-gpu-scheduler"
            element={
              <ProtectedRoute>
                <M9GPUSchedulerPage />
              </ProtectedRoute>
            }
          />
          
          {/* M10 RL-Based Training Optimizer */}
          <Route
            path="/m10-rl-optimizer"
            element={
              <ProtectedRoute>
                <M10RLOptimizerPage />
              </ProtectedRoute>
            }
          />
          
          {/* M11 Multi-tenant GPU Sharing & Quota Management */}
          <Route
            path="/m11-gpu-sharing"
            element={
              <ProtectedRoute>
                <M11MultiTenantGPUSharingPage />
              </ProtectedRoute>
            }
          />
          
          {/* M1 Distributed Ledger - Verifiable Control Plane */}
          <Route
            path="/m1-distributed-ledger"
            element={
              <ProtectedRoute>
                <M1DistributedLedgerPage />
              </ProtectedRoute>
            }
          />
          
          {/* M3 Fabric Connectivity - Event Mesh Dashboard */}
          <Route
            path="/m3-fabric"
            element={
              <ProtectedRoute>
                <M3FabricConnectivityPage />
              </ProtectedRoute>
            }
          />
          
          {/* M4 Security Baseline - RBAC & Compliance */}
          <Route
            path="/m4-security"
            element={
              <ProtectedRoute>
                <M4SecurityBaselinePage />
              </ProtectedRoute>
            }
          />
          
          {/* M6 Feature Store - ML Feature Registry & Serving */}
          <Route
            path="/m6-feature-store"
            element={
              <ProtectedRoute>
                <M6FeatureStorePage />
              </ProtectedRoute>
            }
          />
          
          {/* M2 Model Lifecycle Management - AI/ML Model Registry */}
          <Route
            path="/m2-model-lifecycle"
            element={
              <ProtectedRoute>
                <M2ModelLifecyclePage />
              </ProtectedRoute>
            }
          />
          
          {/* M17 AutoML Platform - Hyperparameter Tuning & Neural Architecture Search */}
          <Route
            path="/m17-automl"
            element={
              <ProtectedRoute>
                <M17AutoMLPlatformPage />
              </ProtectedRoute>
            }
          />
          
          {/* M18 Experiment Tracker - ML Experiment Tracking & Comparison */}
          <Route
            path="/m18-experiment-tracker"
            element={
              <ProtectedRoute>
                <M18ExperimentTrackerPage />
              </ProtectedRoute>
            }
          />
          
          {/* M19 Behavior Hunting - Security Threat Detection & UEBA */}
          <Route
            path="/m19-behavior-hunting"
            element={
              <ProtectedRoute>
                <M19BehaviorHuntingPage />
              </ProtectedRoute>
            }
          />
          
          {/* M20 Federated Learning - Distributed Privacy-Preserving AI */}
          <Route
            path="/m20-federated-learning"
            element={
              <ProtectedRoute>
                <M20FederatedLearningPage />
              </ProtectedRoute>
            }
          />
          
          {/* M36 AI-Powered Vulnerability Management */}
          <Route
            path="/m36-vuln-mgmt"
            element={
              <ProtectedRoute>
                <M36AIVulnerabilityManagementPage />
              </ProtectedRoute>
            }
          />
          
          {/* M37 DevSecOps Pipeline Integration */}
          <Route
            path="/m37-devsecops"
            element={
              <ProtectedRoute>
                <M37DevSecOpsPipelinePage />
              </ProtectedRoute>
            }
          />
          
          {/* M38 Container Security Platform */}
          <Route
            path="/m38-container-security"
            element={
              <ProtectedRoute>
                <M38ContainerSecurityPlatformPage />
              </ProtectedRoute>
            }
          />
          
          {/* M39 Identity & Access Management */}
          <Route
            path="/m39-iam"
            element={
              <ProtectedRoute>
                <M39IdentityAccessManagementPage />
              </ProtectedRoute>
            }
          />
          
          {/* M40 API Security Gateway */}
          <Route
            path="/m40-api-gateway"
            element={
              <ProtectedRoute>
                <M40APISecurityGatewayPage />
              </ProtectedRoute>
            }
          />
          
          {/* Fallback routes */}
          <Route path="/engagements/:id" element={<ProtectedRoute><DashboardPage /></ProtectedRoute>} />
          <Route path="/engagements/:id/report" element={<ProtectedRoute><ReportsPage /></ProtectedRoute>} />
          <Route path="/engagements/:id/evidence" element={<ProtectedRoute><ReportsPage /></ProtectedRoute>} />
          
          {/* Home redirect */}
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          
          {/* 404 fallback */}
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

export default App;
