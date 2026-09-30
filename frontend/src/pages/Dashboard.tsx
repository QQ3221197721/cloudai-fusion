import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Activity, Lock, Zap, Database, Globe, Loader2 } from "lucide-react";
import { Link } from "react-router-dom";
import { apiClient } from "@/lib/api";
import { useAuthStore } from "@/stores/auth-store";

interface DashboardStats {
  totalScans: number;
  activeCampaigns: number;
  threatsFound: number;
  pendingOrders: number;
  completedScans: number;
}

interface StatCardProps {
  title: string;
  value: number;
  icon: React.ReactNode;
  color: string;
  trend?: string;
}

const StatCard = ({ title, value, icon, color, trend }: StatCardProps) => (
  <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
    <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
      <p className="text-sm font-medium text-gray-400">{title}</p>
      <div className={`p-2 rounded-lg ${color}`}>
        {icon}
      </div>
    </CardHeader>
    <CardContent>
      <div className="text-3xl font-bold tracking-tight">{value.toLocaleString()}</div>
      {trend && (
        <p className="text-xs text-green-500 mt-1">
          ↑ {trend} from last month
        </p>
      )}
    </CardContent>
  </Card>
);

export function DashboardPage() {
  const navigate = useNavigate();
  const user = useAuthStore((state) => state.user);
  
  const { data: stats, isLoading } = useQuery<DashboardStats>({
    queryKey: ["dashboard_stats"],
    queryFn: () => apiClient.getDashboardStats(),
    placeholderData: {
      totalScans: 0,
      activeCampaigns: 0,
      threatsFound: 0,
      pendingOrders: 0,
      completedScans: 0,
    },
  });
  
  if (isLoading || !user) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900">
        <Loader2 className="animate-spin rounded-full h-12 w-12 border-t-2 border-b-2 border-red-600" />
      </div>
    );
  }
  
  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900">
      {/* Navigation Header */}
      <header className="sticky top-0 z-50 glass-effect backdrop-blur-xl border-b border-slate-700/50">
        <div className="container mx-auto px-4 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Activity className="w-8 h-8 text-red-600" strokeWidth={2} />
              <h1 className="text-2xl font-bold bg-gradient-to-r from-red-500 to-orange-500 bg-clip-text text-transparent">CloudAI Fusion</h1>
            </div>
            
            <nav className="flex items-center gap-4">
              <Button variant="ghost" asChild className="text-gray-300 hover:text-white">
                <Link to="/quickscan">Quick Scan</Link>
              </Button>
              <Button variant="ghost" asChild className="text-gray-300 hover:text-white">
                <Link to="/campaigns">Campaigns</Link>
              </Button>
              <Button variant="ghost" asChild className="text-gray-300 hover:text-white">
                <Link to="/reports">Reports</Link>
              </Button>
              <Button variant="outline" className="border-slate-700 hover:bg-slate-800 text-white" onClick={() => navigate("/workorder-submit")}>
                New Work Order
              </Button>
            </nav>
          </div>
        </div>
      </header>
      
      {/* Main Content */}
      <main className="container mx-auto px-4 py-8">
        {/* Welcome Section */}
        <div className="mb-8">
          <h2 className="text-3xl font-bold text-white mb-2">Welcome back, {user.username}</h2>
          <p className="text-gray-400">Here's what's happening with your red team operations today.</p>
        </div>
        
        {/* Stats Grid */}
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4 mb-8">
          <StatCard
            title="Total Scans"
            value={stats?.totalScans || 0}
            icon={<Activity className="w-5 h-5 text-blue-400" />}
            color="bg-blue-500/10"
            trend="12%"
          />
          
          <StatCard
            title="Active Campaigns"
            value={stats?.activeCampaigns || 0}
            icon={<Zap className="w-5 h-5 text-orange-400" />}
            color="bg-orange-500/10"
            trend="3"
          />
          
          <StatCard
            title="Threats Found"
            value={stats?.threatsFound || 0}
            icon={<Activity className="w-5 h-5 text-red-400" />}
            color="bg-red-500/10"
            trend="7"
          />
          
          <StatCard
            title="Pending Orders"
            value={stats?.pendingOrders || 0}
            icon={<Database className="w-5 h-5 text-yellow-400" />}
            color="bg-yellow-500/10"
          />
        </div>
        
        {/* Action Cards */}
        <div className="grid lg:grid-cols-3 gap-8">
          {/* Quick Actions */}
          <div className="lg:col-span-2 space-y-6">
            <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
              <CardHeader>
                <p className="text-xl text-white flex items-center gap-2">
                  <Activity className="w-5 h-5 text-red-600" />
                  Quick Actions
                </p>
              </CardHeader>
              <CardContent>
                <Tabs defaultValue="scan" className="space-y-4">
                  <TabsList className="grid w-full grid-cols-5 bg-slate-800/50">
                    <TabsTrigger value="scan" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">
                      🔍 Quick Scan
                    </TabsTrigger>
                    <TabsTrigger value="campaign" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">
                      ⚠️ Attack Campaign
                    </TabsTrigger>
                    <TabsTrigger value="workorder" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">
                      📋 Work Order
                    </TabsTrigger>
                    <TabsTrigger value="evidence" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">
                      📊 Evidence
                    </TabsTrigger>
                    <TabsTrigger value="raft" className="data-[state=active]:bg-red-600 data-[state=active]:text-white">
                      🏛️ Raft Cluster
                    </TabsTrigger>
                  </TabsList>
                  
                  <TabsContent value="scan">
                    <Card className="bg-slate-800/30 border-slate-700/50">
                      <CardContent className="pt-6">
                        <Link to="/quickscan">
                          <Button size="lg" className="bg-red-600 hover:bg-red-700 text-white w-full py-6 text-base">
                            Launch Vulnerability Scan →
                          </Button>
                        </Link>
                      </CardContent>
                    </Card>
                  </TabsContent>
                  
                  <TabsContent value="campaign">
                    <Card className="bg-slate-800/30 border-slate-700/50">
                      <CardContent className="pt-6">
                        <Button size="lg" variant="outline" className="w-full py-6 text-base" asChild={{ isForwardRef: false }}>
                          <Link to="/campaigns">Manage Attack Campaigns →</Link>
                        </Button>
                      </CardContent>
                    </Card>
                  </TabsContent>
                  
                  <TabsContent value="workorder">
                    <Card className="bg-slate-800/30 border-slate-700/50">
                      <CardContent className="pt-6">
                        <Button size="lg" variant="outline" className="w-full py-6 text-base" asChild={{ isForwardRef: false }}>
                          <Link to="/workorder-submit">Submit Production Request →</Link>
                        </Button>
                      </CardContent>
                    </Card>
                  </TabsContent>
                  
                  <TabsContent value="evidence">
                    <Card className="bg-slate-800/30 border-slate-700/50">
                      <CardContent className="pt-6">
                        <Button size="lg" variant="outline" className="w-full py-6 text-base" asChild={{ isForwardRef: false }}>
                          <Link to="/reports">View Verifiable Reports →</Link>
                        </Button>
                      </CardContent>
                    </Card>
                  </TabsContent>
                  
                  <TabsContent value="raft">
                    <Card className="bg-slate-800/30 border-slate-700/50">
                      <CardContent className="pt-6">
                        <Button size="lg" variant="outline" className="w-full py-6 text-base" asChild={{ isForwardRef: false }}>
                          <Link to="/m7-raft-consensus">Launch Raft Consensus Monitor →</Link>
                        </Button>
                      </CardContent>
                    </Card>
                  </TabsContent>
                </Tabs>
              </CardContent>
            </Card>
          </div>
          
          {/* System Status */}
          <div className="space-y-6">
            <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
              <CardHeader>
                <p className="text-lg text-white">System Status</p>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-center justify-between">
                  <span className="text-sm text-gray-400">Engine Status</span>
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full bg-green-500 animate-pulse" />
                    <span className="text-sm text-green-400">Running</span>
                  </div>
                </div>
                
                <div className="flex items-center justify-between">
                  <span className="text-sm text-gray-400">Security Gate</span>
                  <div className="flex items-center gap-2">
                    <Lock className="w-4 h-4 text-red-500" />
                    <span className="text-sm text-red-400">Active</span>
                  </div>
                </div>
                
                <div className="pt-4 border-t border-slate-700/50">
                  <p className="text-xs text-gray-500">
                    All systems operational • Last updated just now
                  </p>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
      </main>
    </div>
  );
}
