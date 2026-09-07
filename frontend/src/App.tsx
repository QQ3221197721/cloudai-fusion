import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useAuthStore } from "@/stores/auth-store";
import { LoginPage } from "@/pages/Login";
import { DashboardPage } from "@/pages/Dashboard";
import { WorkOrderSubmitPage } from "@/pages/WorkOrderSubmit";
import { CampaignsPage } from "@/pages/Campaigns";
import { ReportsPage } from "@/pages/Reports";

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
