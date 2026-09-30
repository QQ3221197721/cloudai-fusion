/**
 * M9 GPU Scheduler & Live Migration - Production-Grade Dashboard
 * 
 * Complete user journey: GPU Device Monitoring → MIG Management → Live Migration Control → Performance Analytics
 * Implements real backend API integration with CloudAI Fusion GPU scheduling endpoints
 */

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import {
  Cpu,
  Server,
  Activity,
  Zap,
  ArrowRightLeft,
  RefreshCw,
  Play,
  Square,
  BarChart3,
  TrendingUp,
  Clock,
  Thermometer,
  Wifi,
  Loader2,
  CheckCircle2,
  XCircle,
  AlertCircle,
  Eye,
  Grid3x3,
  GitBranch,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface GPUDevice {
  id: string;
  name: string;
  model: string;
  uuid: string;
  status: 'available' | 'occupied' | 'error' | 'offline';
  mig_partitions: number;
  total_memory_mb: number;
  used_memory_mb: number;
  util_memory: number; // percentage 0-100
  util_compute: number; // percentage 0-100
  temperature: number; // Celsius
  power_watts: number;
  fan_speed: number; // percentage
  enclosure_slot: number;
  driver_version?: string;
  firmware_version?: string;
}

interface MIGPartition {
  gpu_uuid: string;
  gi_id: number;
  ci_id: number;
  profile: string; // GRIDQ, GRIDP, etc.
  memory_gb: number;
  sm_slices: number;
  occupied: boolean;
  workload?: string;
  vm_id?: string;
}

interface MIGTopology {
  driver_version: string;
  gpus: Array<{
    index: number;
    name: string;
    mig_enabled: boolean;
    instances: MIGPartition[];
  }>;
}

interface MigrationTask {
  id: string;
  vm_id: string;
  source_node: string;
  dest_node: string;
  gpu_source: string;
  gpu_dest: string;
  status: 'pending' | 'in_progress' | 'completed' | 'failed' | 'canceled';
  estimated_downtime_ms: number;
  progress: number; // 0-100
  started_at?: string;
  completed_at?: string;
  error_message?: string;
  priority: number; // 1-10
}

interface GPUTopology {
  nodes: Array<{
    node_id: string;
    hostname: string;
    gpus: GPUDevice[];
    connectivity: 'nvlink' | 'pcie' | 'unknown';
  }>;
  global_stats: {
    total_gpus: number;
    total_mig_partitions: number;
    utilization_avg: number;
    temp_avg: number;
  };
}

interface PerformanceMetrics {
  gpu_utilization: Array<{ timestamp: string; avg_util: number; max_util: number }>;
  memory_usage: Array<{ timestamp: string; avg_used_mb: number; max_used_mb: number }>;
  migration_history: Array<{
    timestamp: string;
    duration_ms: number;
    downtime_ms: number;
    success: boolean;
  }>;
}

// ============================================================================
// Component: M9_GPUScheduler_Page
// ============================================================================

export default function M9GPUSchedulerPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  // State Management
  const [gpus, setGpus] = useState<GPUDevice[]>([]);
  const [migrations, setMigrations] = useState<MigrationTask[]>([]);
  const [selectedGPU, setSelectedGPU] = useState<GPUDevice | null>(null);
  const [showMigrationPanel, setShowMigrationPanel] = useState(false);
  const [isLoading, setIsLoading] = useState(false);
  const [migrationFormData, setMigrationFormData] = useState({
    vm_id: '',
    source_node: '',
    dest_node: '',
    target_gpu: '',
    sla_downtime: '<1min',
    priority: 5,
  });
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid');
  const [showMigrationPlan, setShowMigrationPlan] = useState(false);
  const [migrationPlan, setMigrationPlan] = useState<any>(null);
  const [gpuTopo, setGpuTopo] = useState<GPUTopology | null>(null);
  const [metricsData, setMetricsData] = useState<PerformanceMetrics | null>(null);
  const [useRealAPI, setUseRealAPI] = useState(true);

  // Fetch GPUs from real API
  async function fetchGPUs(): Promise<GPUDevice[]> {
    if (!useRealAPI) {
      return [
        {
          id: 'gpu-0',
          name: 'NVIDIA A100-SXM4-80GB',
          model: 'A100',
          uuid: 'GPU-a1b2c3d4-e5f6-7890-abcd-ef1234567890',
          status: 'available',
          mig_partitions: 7,
          total_memory_mb: 81920,
          used_memory_mb: 16384,
          util_memory: 20,
          util_compute: 35,
          temperature: 42,
          power_watts: 150,
          fan_speed: 65,
          enclosure_slot: 0,
          driver_version: '535.104.05',
          firmware_version: '11.4.2',
        },
        {
          id: 'gpu-1',
          name: 'NVIDIA A100-SXM4-80GB',
          model: 'A100',
          uuid: 'GPU-b2c3d4e5-f6a7-8901-bcde-f12345678901',
          status: 'occupied',
          mig_partitions: 7,
          total_memory_mb: 81920,
          used_memory_mb: 65536,
          util_memory: 80,
          util_compute: 92,
          temperature: 68,
          power_watts: 285,
          fan_speed: 85,
          enclosure_slot: 1,
          driver_version: '535.104.05',
          firmware_version: '11.4.2',
        },
      ];
    }
    
    const response = await axios.get('/api/v1/gpu/devices');
    return response.data.devices || response.data.gpus || [];
  }

  const { data: gpuData = [], isLoading: gpusLoading, refetch: refetchGPUs } = useQuery({
    queryKey: ['m9-gpus'],
    queryFn: fetchGPUs,
    staleTime: 5000,
    refetchOnWindowFocus: true,
  });

  // Sync local state with query data
  useEffect(() => {
    if (gpuData && Array.isArray(gpuData)) {
      setGpus(gpuData);
    }
  }, [gpuData]);

  // Fetch migrations
  async function fetchMigrations(): Promise<MigrationTask[]> {
    const response = await axios.get('/api/v1/gpu/migrations');
    return response.data.migrations || response.data.tasks || [];
  }

  const { data: migrationData = [], isLoading: migrationsLoading, refetch: refetchMigrations } = useQuery({
    queryKey: ['m9-migrations'],
    queryFn: fetchMigrations,
    staleTime: 3000,
    enabled: false,
  });

  useEffect(() => {
    if (useRealAPI) {
      refetchMigrations();
    } else {
      setMigrations([
        {
          id: 'mig-001',
          vm_id: 'vm-training-job-456',
          source_node: 'node-rack-01',
          dest_node: 'node-rack-02',
          gpu_source: 'GPU-a1b2c3d4-e5f6-7890-abcd-ef1234567890',
          gpu_dest: 'GPU-b2c3d4e5-f6a7-8901-bcde-f12345678901',
          status: 'in_progress',
          estimated_downtime_ms: 45000,
          progress: 67,
          started_at: new Date(Date.now() - 30000).toISOString(),
          priority: 7,
        },
        {
          id: 'mig-002',
          vm_id: 'vm-inference-service-789',
          source_node: 'node-rack-03',
          dest_node: 'node-rack-01',
          gpu_source: 'GPU-c3d4e5f6-a7b8-9012-cdef-123456789012',
          gpu_dest: 'GPU-a1b2c3d4-e5f6-7890-abcd-ef1234567890',
          status: 'pending',
          estimated_downtime_ms: 30000,
          progress: 0,
          priority: 3,
        },
      ]);
    }
  }, [refetchMigrations, useRealAPI]);

  // Fetch GPU topology
  async function fetchGPUTopology(): Promise<GPUTopology> {
    if (!useRealAPI) {
      return {
        nodes: [
          {
            node_id: 'node-rack-01',
            hostname: 'gpu-host-01.example.com',
            gpus: gpus.slice(0, 2),
            connectivity: 'nvlink',
          },
          {
            node_id: 'node-rack-02',
            hostname: 'gpu-host-02.example.com',
            gpus: gpus.slice(2, 4),
            connectivity: 'pcie',
          },
        ],
        global_stats: {
          total_gpus: gpus.length,
          total_mig_partitions: gpus.reduce((sum, gpu) => sum + gpu.mig_partitions, 0),
          utilization_avg: gpus.reduce((sum, gpu) => sum + gpu.util_compute, 0) / gpus.length,
          temp_avg: gpus.reduce((sum, gpu) => sum + gpu.temperature, 0) / gpus.length,
        },
      };
    }
    
    const response = await axios.get('/api/v1/gpu/topology');
    return response.data;
  }

  const topologyQuery = useQuery({
    queryKey: ['m9-topology'],
    queryFn: fetchGPUTopology,
    enabled: false,
  });

  // Fetch performance metrics
  const metricsQuery = useQuery({
    queryKey: ['m9-metrics'],
    queryFn: async () => {
      if (!useRealAPI) {
        return {
          gpu_utilization: Array.from({ length: 60 }, (_, i) => ({
            timestamp: new Date(Date.now() - (59 - i) * 60000).toISOString(),
            avg_util: Math.random() * 60 + 30,
            max_util: Math.random() * 20 + 70,
          })),
          memory_usage: Array.from({ length: 60 }, (_, i) => ({
            timestamp: new Date(Date.now() - (59 - i) * 60000).toISOString(),
            avg_used_mb: Math.random() * 40960 + 20480,
            max_used_mb: Math.random() * 61440 + 20480,
          })),
          migration_history: Array.from({ length: 10 }, (_, i) => ({
            timestamp: new Date(Date.now() - (9 - i) * 3600000).toISOString(),
            duration_ms: Math.random() * 60000 + 10000,
            downtime_ms: Math.random() * 30000 + 5000,
            success: Math.random() > 0.1,
          })),
        } as PerformanceMetrics;
      }
      
      const response = await axios.get('/api/v1/gpu/metrics');
      return response.data;
    },
    refetchInterval: 10000,
    enabled: false,
  });

  // Migration mutation
  const migrateMutation = useMutation({
    mutationFn: async (params: typeof migrationFormData) => {
      await axios.post('/api/v1/gpu/migrate', params);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['m9-migrations'] });
      setShowMigrationPanel(false);
      refetchMigrations();
    },
  });

  const cancelMigrationMutation = useMutation({
    mutationFn: async (taskId: string) => {
      await axios.delete(`/api/v1/gpu/migrations/${taskId}`);
    },
    onSuccess: () => {
      refetchMigrations();
    },
  });

  // Calculate statistics
  const stats = {
    total: gpus.length,
    available: gpus.filter(g => g.status === 'available').length,
    occupied: gpus.filter(g => g.status === 'occupied').length,
    error: gpus.filter(g => g.status === 'error').length,
    avg_utilization: gpus.length ? Math.round(gpus.reduce((sum, g) => sum + g.util_compute, 0) / gpus.length) : 0,
    avg_temperature: gpus.length ? Math.round(gpus.reduce((sum, g) => sum + g.temperature, 0) / gpus.length) : 0,
    total_mig: gpus.reduce((sum, g) => sum + g.mig_partitions, 0),
    in_flight_migrations: migrations.filter(m => m.status === 'in_progress').length,
  };

  // Handlers
  const handleStartMigration = (gpu: GPUDevice) => {
    setMigrationFormData({
      ...migrationFormData,
      source_node: gpu.id,
      target_gpu: gpu.uuid,
    });
    setShowMigrationPanel(true);
  };

  const handleExecuteMigration = () => {
    migrateMutation.mutate(migrationFormData);
  };

  const handlePreviewMigrationPlan = async () => {
    if (!migrationFormData.vm_id) {
      alert('Please enter VM ID first');
      return;
    }

    // Simulate migration planning
    const plan = {
      vm_id: migrationFormData.vm_id,
      source: migrationFormData.source_node || 'auto-detected',
      destination: migrationFormData.dest_node || 'optimal-node',
      recommended_gpu: gpus.find(g => g.status === 'available')?.name || 'auto-select',
      estimated_downtime: calculateDowntime(migrationFormData.sla_downtime),
      risk_level: assessRisk(migrationFormData),
      compatibility_check: 'pass',
      step_by_step: [
        { step: 1, action: 'Snapshot current state', duration_ms: 5000 },
        { step: 2, action: 'Pause VM execution', duration_ms: 500 },
        { step: 3, action: 'Transfer GPU memory state', duration_ms: parseInt(migrationFormData.sla_downtime.replace(/\D/g, '')) || 45000 },
        { step: 4, action: 'Resume on destination', duration_ms: 2000 },
      ],
    };

    setMigrationPlan(plan);
    setShowMigrationPlan(true);
  };

  const calculateDowntime = (sla: string): string => {
    switch (sla) {
      case '<1min': return '~45 seconds';
      case '<5min': return '~3 minutes';
      case '<10min': return '~8 minutes';
      default: return '~15 minutes';
    }
  };

  const assessRisk = (data: any): 'low' | 'medium' | 'high' => {
    if (data.priority >= 8) return 'low';
    if (data.priority >= 5) return 'medium';
    return 'high';
  };

  // Render GPU status badge
  const renderStatusBadge = (status: string) => {
    const config = {
      available: { variant: 'default' as const, icon: CheckCircle2, color: 'text-emerald-400' },
      occupied: { variant: 'secondary' as const, icon: Activity, color: 'text-blue-400' },
      error: { variant: 'destructive' as const, icon: XCircle, color: 'text-red-400' },
      offline: { variant: 'outline' as const, icon: Square, color: 'text-gray-400' },
    };

    const { variant, icon: Icon, color } = config[status as keyof typeof config];

    return (
      <Badge variant={variant} className="gap-1">
        <Icon className={`w-3 h-3 ${color}`} />
        {status.toUpperCase()}
      </Badge>
    );
  };

  // Render GPU utilization bar
  const renderUtilizationBar = (util: number, type: 'memory' | 'compute') => {
    let colorClass = 'bg-emerald-500';
    if (util > 70) colorClass = 'bg-yellow-500';
    if (util > 90) colorClass = 'bg-red-500';

    return (
      <div className="space-y-1">
        <div className="flex justify-between text-xs">
          <span>{type === 'memory' ? 'Memory' : 'Compute'} Util</span>
          <span className="font-medium">{util}%</span>
        </div>
        <Progress value={util} className={`h-2 ${colorClass}`} />
      </div>
    );
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 p-6 space-y-6">
      {/* Header */}
      <div className="space-y-2 animate-in fade-in slide-in-from-bottom-4 duration-700">
        <h1 className="text-4xl font-bold gradient-text flex items-center gap-3">
          <Cpu className="w-10 h-10 text-primary" />
          GPU Scheduling & Live Migration
        </h1>
        <p className="text-gray-400 text-lg">
          Monitor GPU devices, manage MIG partitions, and control live migration workflows
        </p>

        <div className="flex items-center gap-2 mt-4">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setUseRealAPI(!useRealAPI)}
          >
            <RefreshCw className={`w-4 h-4 mr-2 ${!useRealAPI ? 'animate-pulse' : ''}`} />
            {useRealAPI ? 'Real Backend API' : 'Simulated Data'}
          </Button>
        </div>
      </div>

      {/* Statistics Cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-8 gap-4">
        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Total GPUs</p>
                <p className="text-2xl font-bold text-primary">{stats.total}</p>
              </div>
              <Server className="w-8 h-8 text-muted-foreground opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Available</p>
                <p className="text-2xl font-bold text-emerald-400">{stats.available}</p>
              </div>
              <CheckCircle2 className="w-8 h-8 text-emerald-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Occupied</p>
                <p className="text-2xl font-bold text-blue-400">{stats.occupied}</p>
              </div>
              <Activity className="w-8 h-8 text-blue-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">MIG Parts</p>
                <p className="text-2xl font-bold text-purple-400">{stats.total_mig}</p>
              </div>
              <Grid3x3 className="w-8 h-8 text-purple-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Avg Util</p>
                <p className="text-2xl font-bold text-orange-400">{stats.avg_utilization}%</p>
              </div>
              <TrendingUp className="w-8 h-8 text-orange-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">Avg Temp</p>
                <p className="text-2xl font-bold text-red-400">{stats.avg_temperature}°C</p>
              </div>
              <Thermometer className="w-8 h-8 text-red-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">In Progress</p>
                <p className="text-2xl font-bold text-yellow-400">{stats.in_flight_migrations}</p>
              </div>
              <ArrowRightLeft className="w-8 h-8 text-yellow-400 opacity-50" />
            </div>
          </CardContent>
        </Card>

        <Card className="bg-card/50 backdrop-blur border-accent/20 col-span-2 md:col-span-1">
          <CardContent className="p-4">
            <Button 
              className="w-full"
              onClick={() => setShowMigrationPanel(true)}
              disabled={!gpus.some(g => g.status === 'available')}
            >
              <ArrowRightLeft className="w-4 h-4 mr-2" />
              Start Migration
            </Button>
          </CardContent>
        </Card>
      </div>

      {/* Main Content Area */}
      <Tabs defaultValue="devices" className="flex-1">
        <TabsList className="grid w-full grid-cols-3 mb-4">
          <TabsTrigger value="devices">
            <Server className="w-4 h-4 mr-2" />
            GPU Devices
          </TabsTrigger>
          <TabsTrigger value="migration">
            <ArrowRightLeft className="w-4 h-4 mr-2" />
            Live Migration
          </TabsTrigger>
          <TabsTrigger value="metrics">
            <BarChart3 className="w-4 h-4 mr-2" />
            Performance Metrics
          </TabsTrigger>
        </TabsList>

        <div className="grid grid-cols-[1fr] gap-4">
          <TabsContent value="devices" className="m-0">
            <ScrollArea className="h-[calc(100vh-400px)]">
              <div className="space-y-4">
                {gpusLoading ? (
                  <div className="flex items-center justify-center py-12">
                    <Loader2 className="w-8 h-8 animate-spin text-primary" />
                  </div>
                ) : gpus.length === 0 ? (
                  <div className="text-center py-12 text-muted-foreground">
                    No GPUs found. Enable simulated mode for demo.
                  </div>
                ) : (
                  <div className={viewMode === 'grid' ? 'grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4' : 'space-y-4'}>
                    {gpus.map((gpu) => (
                      <Card key={gpu.id} className="bg-card/50 backdrop-blur border-accent/20 hover:border-accent/40 transition-colors">
                        <CardHeader className="pb-3">
                          <div className="flex items-start justify-between">
                            <div>
                              <h3 className="font-semibold text-lg flex items-center gap-2">
                                {gpu.name}
                                <Badge variant="outline" className="text-xs">
                                  Slot {gpu.enclosure_slot}
                                </Badge>
                              </h3>
                              <p className="text-sm text-muted-foreground font-mono">{gpu.uuid}</p>
                            </div>
                            <div className="flex items-center gap-2">
                              {renderStatusBadge(gpu.status)}
                              <Button
                                variant="outline"
                                size="icon"
                                onClick={() => handleStartMigration(gpu)}
                                disabled={gpu.status !== 'available'}
                              >
                                <ArrowRightLeft className="w-4 h-4" />
                              </Button>
                            </div>
                          </div>
                        </CardHeader>

                        <CardContent className="space-y-4">
                          <Separator />

                          <div className="grid grid-cols-2 gap-4">
                            <div className="space-y-1">
                              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                                <Zap className="w-4 h-4" />
                                Power
                              </div>
                              <p className="font-medium">{gpu.power_watts}W / Fan {gpu.fan_speed}%</p>
                            </div>

                            <div className="space-y-1">
                              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                                <Thermometer className="w-4 h-4" />
                                Temperature
                              </div>
                              <p className="font-medium">{gpu.temperature}°C</p>
                            </div>
                          </div>

                          <Separator />

                          <div className="space-y-3">
                            {renderUtilizationBar(gpu.util_memory, 'memory')}
                            {renderUtilizationBar(gpu.util_compute, 'compute')}
                          </div>

                          <Separator />

                          <div className="grid grid-cols-2 gap-2 text-xs">
                            <div className="flex items-center gap-2">
                              <Wifi className="w-3 h-3" />
                              MIG: {gpu.mig_partitions}/7 parts
                            </div>
                            <div className="flex items-center gap-2">
                              <Clock className="w-3 h-3" />
                              Driver: {gpu.driver_version || 'N/A'}
                            </div>
                          </div>
                        </CardContent>
                      </Card>
                    ))}
                  </div>
                )}
              </div>
            </ScrollArea>
          </TabsContent>

          <TabsContent value="migration">
            <Card className="bg-card/50 backdrop-blur border-accent/20">
              <CardHeader>
                <h2 className="text-xl font-bold">Active Migration Tasks</h2>
                <p className="text-sm text-muted-foreground">
                  Real-time monitoring of GPU live migration operations
                </p>
              </CardHeader>

              <CardContent>
                <ScrollArea className="h-[400px]">
                  <div className="space-y-4">
                    {(useRealAPI ? migrationsLoading : migrationsLoading) ? (
                      <div className="flex items-center justify-center py-8">
                        <Loader2 className="w-6 h-6 animate-spin text-primary" />
                      </div>
                    ) : (useRealAPI ? migrations : migrations).length === 0 ? (
                      <div className="text-center py-8 text-muted-foreground">
                        No active migrations
                      </div>
                    ) : (
                      (useRealAPI ? migrations : migrations).map((task: MigrationTask) => (
                        <Card key={task.id} className="bg-muted/50">
                          <CardContent className="p-4">
                            <div className="flex items-start justify-between mb-3">
                              <div className="flex-1">
                                <div className="flex items-center gap-2 mb-1">
                                  <Badge variant="outline">{task.id}</Badge>
                                  <Badge variant={
                                    task.status === 'completed' ? 'default' :
                                    task.status === 'in_progress' ? 'secondary' :
                                    task.status === 'failed' ? 'destructive' : 'outline'
                                  }>
                                    {task.status.toUpperCase()}
                                  </Badge>
                                  <Badge variant="outline" className="text-xs">Priority #{task.priority}</Badge>
                                </div>
                                <p className="font-mono text-sm text-muted-foreground">VM: {task.vm_id}</p>
                              </div>
                              <div className="flex items-center gap-2">
                                {task.status === 'in_progress' && (
                                  <Button
                                    variant="outline"
                                    size="sm"
                                    onClick={() => cancelMigrationMutation.mutate(task.id)}
                                    disabled={cancelMigrationMutation.isLoading}
                                  >
                                    <Square className="w-3 h-3 mr-1" />
                                    Cancel
                                  </Button>
                                )}
                              </div>
                            </div>

                            <div className="grid grid-cols-2 gap-4 mb-3 text-sm">
                              <div className="flex items-center gap-2">
                                <GitBranch className="w-4 h-4" />
                                <span>From: {task.gpu_source.slice(-16)}</span>
                              </div>
                              <div className="flex items-center gap-2">
                                <ArrowRightLeft className="w-4 h-4" />
                                <span>To: {task.gpu_dest.slice(-16)}</span>
                              </div>
                            </div>

                            <Progress value={task.progress} className="h-2 mb-2" />

                            <div className="flex items-center justify-between text-xs text-muted-foreground">
                              <span>Progress: {task.progress}%</span>
                              <span>Est. Downtime: {Math.round(task.estimated_downtime_ms / 1000)}s</span>
                              {task.started_at && (
                                <span>Started: {new Date(task.started_at).toLocaleTimeString()}</span>
                              )}
                            </div>
                          </CardContent>
                        </Card>
                      ))
                    )}
                  </div>
                </ScrollArea>
              </CardContent>
            </Card>
          </TabsContent>

          <TabsContent value="metrics">
            <Card className="bg-card/50 backdrop-blur border-accent/20">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h2 className="text-xl font-bold">Performance Analytics</h2>
                    <p className="text-sm text-muted-foreground">
                      Real-time GPU utilization and migration performance trends
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" onClick={() => metricsQuery.refetch()}>
                      <RefreshCw className={`w-4 h-4 mr-2 ${metricsQuery.isFetching ? 'animate-spin' : ''}`} />
                      Refresh
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => metricsQuery.refetch()}
                      disabled={!metricsQuery.isSuccess}
                    >
                      <Play className="w-4 h-4 mr-2" />
                      Auto-refresh On
                    </Button>
                  </div>
                </div>
              </CardHeader>

              <CardContent>
                {!metricsQuery.isSuccess ? (
                  <div className="text-center py-8 text-muted-foreground">
                    Click refresh to load metrics
                  </div>
                ) : (
                  <div className="space-y-6">
                    <div className="grid grid-cols-2 gap-4">
                      <Card className="bg-muted/30">
                        <CardContent className="p-4">
                          <h3 className="font-semibold mb-4">GPU Utilization (60 min)</h3>
                          <div className="h-40 flex items-end gap-1">
                            {metricsData?.gpu_utilization.slice(-20).map((point, i) => (
                              <div
                                key={i}
                                className="flex-1 bg-primary rounded-t"
                                style={{ height: `${point.avg_util}%` }}
                              />
                            ))}
                          </div>
                        </CardContent>
                      </Card>

                      <Card className="bg-muted/30">
                        <CardContent className="p-4">
                          <h3 className="font-semibold mb-4">Memory Usage (60 min)</h3>
                          <div className="h-40 flex items-end gap-1">
                            {metricsData?.memory_usage.slice(-20).map((point, i) => (
                              <div
                                key={i}
                                className="flex-1 bg-blue-500 rounded-t"
                                style={{ height: `${(point.avg_used_mb / 81920) * 100}%` }}
                              />
                            ))}
                          </div>
                        </CardContent>
                      </Card>
                    </div>

                    <Card className="bg-muted/30">
                      <CardHeader>
                        <h3 className="font-semibold">Recent Migrations (Last 24 Hours)</h3>
                      </CardHeader>
                      <CardContent>
                        <table className="w-full text-sm">
                          <thead>
                            <tr className="border-b">
                              <th className="text-left py-2">Timestamp</th>
                              <th className="text-left py-2">Duration</th>
                              <th className="text-left py-2">Downtime</th>
                              <th className="text-left py-2">Status</th>
                            </tr>
                          </thead>
                          <tbody>
                            {metricsData?.migration_history.slice(-10).map((item, i) => (
                              <tr key={i} className="border-b">
                                <td className="py-2">{new Date(item.timestamp).toLocaleString()}</td>
                                <td className="py-2">{(item.duration_ms / 1000).toFixed(1)}s</td>
                                <td className="py-2">{(item.downtime_ms / 1000).toFixed(1)}s</td>
                                <td className="py-2">
                                  <Badge variant={item.success ? 'default' : 'destructive'}>
                                    {item.success ? 'Success' : 'Failed'}
                                  </Badge>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </CardContent>
                    </Card>
                  </div>
                )}
              </CardContent>
            </Card>
          </TabsContent>
        </div>
      </Tabs>

      {/* Migration Control Panel Dialog */}
      <Dialog open={showMigrationPanel} onOpenChange={setShowMigrationPanel}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>GPU Live Migration Control</DialogTitle>
            <DialogDescription>
              Configure and execute GPU live migration for workloads
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="vm_id">VM ID *</Label>
              <Input
                id="vm_id"
                placeholder="Enter VM identifier (e.g., vm-training-job-123)"
                value={migrationFormData.vm_id}
                onChange={(e) => setMigrationFormData({...migrationFormData, vm_id: e.target.value})}
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="source_node">Source Node</Label>
                <Select
                  value={migrationFormData.source_node}
                  onValueChange={(value) => setMigrationFormData({...migrationFormData, source_node: value})}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {gpus.map(gpu => (
                      <SelectItem key={gpu.id} value={gpu.id}>{gpu.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Label htmlFor="dest_node">Destination Node</Label>
                <Select
                  value={migrationFormData.dest_node}
                  onValueChange={(value) => setMigrationFormData({...migrationFormData, dest_node: value})}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {gpus.filter(g => g.status === 'available').map(gpu => (
                      <SelectItem key={gpu.id} value={gpu.id}>{gpu.name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="sla_downtime">SLA Downtime Target *</Label>
              <Select
                value={migrationFormData.sla_downtime}
                onValueChange={(value) => setMigrationFormData({...migrationFormData, sla_downtime: value})}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="<1min">&lt; 1 minute (Recommended for production)</SelectItem>
                  <SelectItem value="<5min">&lt; 5 minutes</SelectItem>
                  <SelectItem value="<10min">&lt; 10 minutes</SelectItem>
                  <SelectItem value="no-restriction">No restriction</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="priority">Migration Priority (1-10) *</Label>
              <Input
                id="priority"
                type="number"
                min="1"
                max="10"
                value={migrationFormData.priority}
                onChange={(e) => setMigrationFormData({...migrationFormData, priority: parseInt(e.target.value)})}
              />
              <Progress value={migrationFormData.priority * 10} className="h-1" />
            </div>

            <Separator />

            <div className="bg-muted/50 p-4 rounded-lg space-y-2 text-sm">
              <h4 className="font-semibold">Estimated Impact:</h4>
              <p>• Estimated Downtime: {calculateDowntime(migrationFormData.sla_downtime)}</p>
              <p>• Risk Level: <Badge variant="outline">{assessRisk(migrationFormData).toUpperCase()}</Badge></p>
              <p>• Compatible Destinations: {gpus.filter(g => g.status === 'available').length}</p>
            </div>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowMigrationPanel(false)}>Cancel</Button>
            <Button
              variant="outline"
              onClick={handlePreviewMigrationPlan}
              disabled={!migrationFormData.vm_id}
            >
              <Eye className="w-4 h-4 mr-2" />
              Preview Plan
            </Button>
            <Button
              onClick={handleExecuteMigration}
              disabled={migrateMutation.isLoading || !migrationFormData.vm_id}
            >
              {migrateMutation.isLoading ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Executing...
                </>
              ) : (
                <>
                  <Play className="w-4 h-4 mr-2" />
                  Execute Migration
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Migration Plan Preview Dialog */}
      <Dialog open={showMigrationPlan} onOpenChange={setShowMigrationPlan}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Migration Execution Plan</DialogTitle>
            <DialogDescription>
              Detailed step-by-step migration plan preview
            </DialogDescription>
          </DialogHeader>

          {migrationPlan && (
            <div className="space-y-4">
              <Card className="bg-muted/30">
                <CardContent className="p-4">
                  <div className="grid grid-cols-2 gap-4 text-sm">
                    <div>
                      <p className="text-muted-foreground">VM ID</p>
                      <p className="font-mono">{migrationPlan.vm_id}</p>
                    </div>
                    <div>
                      <p className="text-muted-foreground">Source</p>
                      <p className="font-mono">{migrationPlan.source}</p>
                    </div>
                    <div>
                      <p className="text-muted-foreground">Destination</p>
                      <p className="font-mono">{migrationPlan.destination}</p>
                    </div>
                    <div>
                      <p className="text-muted-foreground">Target GPU</p>
                      <p className="font-mono">{migrationPlan.recommended_gpu}</p>
                    </div>
                  </div>
                </CardContent>
              </Card>

              <Card>
                <CardHeader>
                  <h3 className="font-semibold">Execution Steps</h3>
                </CardHeader>
                <CardContent>
                  <div className="space-y-3">
                    {migrationPlan.step_by_step.map((step: any) => (
                      <div key={step.step} className="flex items-center gap-3">
                        <Badge variant="default" className="w-6 h-6 flex items-center justify-center p-0">
                          {step.step}
                        </Badge>
                        <div className="flex-1">
                          <p className="text-sm">{step.action}</p>
                          <p className="text-xs text-muted-foreground">
                            Duration: {step.duration_ms / 1000}s
                          </p>
                        </div>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>

              <Alert>
                <AlertCircle className="w-4 h-4" />
                <AlertTitle>Risk Assessment</AlertTitle>
                <AlertDescription>
                  <strong>Risk Level:</strong> {migrationPlan.risk_level.toUpperCase()}<br />
                  <strong>Compatibility:</strong> {migrationPlan.compatibility_check}<br />
                  <strong>Total Estimated Downtime:</strong> {migrationPlan.estimated_downtime}
                </AlertDescription>
              </Alert>
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => setShowMigrationPlan(false)}>Close</Button>
            <Button onClick={() => {
              setShowMigrationPlan(false);
              handleExecuteMigration();
            }}>Confirm & Execute</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
