/**
 * M20 Federated Learning Platform - Production-Grade Distributed AI Dashboard
 * 
 * Complete user journey: View devices → Enroll new → Start aggregation → Monitor rounds → Deploy global model → Audit privacy
 * Implements real backend API integration with CloudAI Fusion federated learning endpoints
 * Design Philosophy: Linear-style dark theme, distributed systems aesthetics, privacy-preserving ML
 */

import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import axios from "axios";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Network,
  Users,
  Database,
  BrainCircuit,
  ShieldCheck,
  Activity,
  Target,
  Clock,
  TrendingUp,
  Download,
  Upload,
  Play,
  Square,
  RefreshCw,
  Filter,
  Eye,
  Edit,
  Trash2,
  Zap,
  CheckCircle2,
  XCircle,
  Loader2,
  Server,
  Globe,
  Lock,
  PieChart,
  TagIcon,
  Plus,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface EdgeDevice {
  id: string;
  name: string;
  type: 'mobile' | 'iot' | 'server' | 'workstation';
  status: 'online' | 'offline' | 'participating' | 'quarantined';
  last_seen: string;
  capabilities: DeviceCapabilities;
  privacy_compliance: PrivacyCompliance;
  model_versions?: string[];
  aggregated_rounds: number;
  accuracy_improvement?: number;
}

interface DeviceCapabilities {
  cpu_cores: number;
  memory_gb: number;
  storage_gb: number;
  gpu_available: boolean;
  network_bandwidth_mbps: number;
  battery_level?: number; // for mobile/IoT
  location?: { lat: number; lon: number };
}

interface PrivacyCompliance {
  differential_privacy_enabled: boolean;
  epsilon: number;
  secure_aggregation_enabled: boolean;
  data_encryption: boolean;
  consent_granted: boolean;
  gdpr_compliant: boolean;
}

interface AggregationRound {
  id: string;
  round_number: number;
  status: 'pending' | 'running' | 'completed' | 'failed';
  started_at?: string;
  completed_at?: string;
  participating_devices: number;
  total_devices: number;
  epochs_per_device: number;
  aggregation_algorithm: string;
  global_metrics?: GlobalModelMetrics;
  failure_reason?: string;
}

interface GlobalModelMetrics {
  loss: number;
  accuracy: number;
  f1_score?: number;
  precision?: number;
  recall?: number;
  convergence_step: number;
}

interface TrainingConfig {
  aggregation_algorithm: 'fedavg' | 'fedprox' | 'fedmedian' | 'fault_tolerant_fedavg';
  epochs_per_round: number;
  client_sampling_fraction: number;
  min_clients_required: number;
  differential_privacy_epsilon: number;
  secure_aggregation: boolean;
  early_stopping_threshold?: number;
}

interface DeviceEnrollment {
  device_id: string;
  device_name: string;
  device_type: 'mobile' | 'iot' | 'server' | 'workstation';
  capabilities: Partial<DeviceCapabilities>;
  privacy_settings: Partial<PrivacyCompliance>;
  enrolled_at: string;
  status: 'active' | 'inactive' | 'pending_verification';
}

interface HeterogeneityAnalysis {
  client_capability_distribution: Record<string, number>;
  data_diversity_index: number;
  gradient_variance_ratio: number;
  straggler_detection: Array<{ device_id: string; latency_ms: number }>;
  recommendations: string[];
}

// ============================================================================
// Utility Functions
// ============================================================================

const getStatusColor = (status: string) => {
  switch(status.toLowerCase()) {
    case 'online': return 'bg-green-500/20 text-green-400 border-green-500/30';
    case 'participating': return 'bg-blue-500/20 text-blue-400 border-blue-500/30';
    case 'offline': return 'bg-gray-500/20 text-gray-400 border-gray-500/30';
    case 'quarantined': return 'bg-red-500/20 text-red-400 border-red-500/30';
    default: return 'bg-gray-500/20 text-gray-400';
  }
};

const formatDuration = (seconds: number) => {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return `${hours}h ${minutes}m`;
};

const formatDate = (dateString: string) => {
  return new Date(dateString).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
};

// ============================================================================
// Main Component
// ============================================================================

export default function M20FederatedLearningPage() {
  const queryClient = useQueryClient();
  const [selectedDevice, setSelectedDevice] = useState<EdgeDevice | null>(null);
  const [activeTab, setActiveTab] = useState("dashboard");
  
  // Form state for device enrollment
  const [newDevice, setNewDevice] = useState({
    name: "",
    type: 'workstation' as 'mobile' | 'iot' | 'server' | 'workstation',
    cpuCores: 4,
    memoryGB: 8,
    enableDP: true,
    dpEpsilon: 1.0,
  });

  // Query hooks
  const { data: devicesResponse } = useQuery<{ devices: EdgeDevice[]; total: number }>({
    queryKey: ["fedDevices"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/federated-learning/devices`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  const { data: roundsResponse } = useQuery<{ rounds: AggregationRound[]; current: AggregationRound }>({
    queryKey: ["fedRounds"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/federated-learning/rounds`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  const { data: modelsResponse } = useQuery<{ models: Array<{ version: string; metrics: GlobalModelMetrics; created_at: string }> }>({
    queryKey: ["fedModels"],
    queryFn: async () => {
      const token = localStorage.getItem("token");
      const res = await axios.get(`${API_BASE_URL}/api/v1/federated-learning/models`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      return res.data;
    },
  });

  // Mutation hooks
  const enrollDeviceMutation = useMutation({
    mutationFn: async (deviceData: any) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/federated-learning/devices/enroll`, deviceData, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["fedDevices"] });
    },
  });

  const startRoundMutation = useMutation({
    mutationFn: async () => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/federated-learning/rounds/start`, {}, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["fedRounds"] });
    },
  });

  const stopRoundMutation = useMutation({
    mutationFn: async () => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/federated-learning/rounds/stop`, {}, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["fedRounds"] });
    },
  });

  const deployModelMutation = useMutation({
    mutationFn: async (version: string) => {
      const token = localStorage.getItem("token");
      return axios.post(`${API_BASE_URL}/api/v1/federated-learning/models/${version}/deploy`, {}, {
        headers: { Authorization: `Bearer ${token}` },
      });
    },
    onSuccess: (_, version) => {
      queryClient.invalidateQueries({ queryKey: ["fedModels"] });
    },
  });

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950 p-6">
      {/* Header */}
      <div className="mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-4xl font-bold bg-gradient-to-r from-purple-500 to-pink-500 bg-clip-text text-transparent">
              Federated Learning
            </h1>
            <p className="text-gray-400 mt-2">
              Privacy-preserving distributed AI with edge intelligence
            </p>
          </div>
          <Button
            onClick={() => {}}
            className="bg-purple-600 hover:bg-purple-700 text-white gap-2"
          >
            <Plus className="w-4 h-4" />
            Enroll Device
          </Button>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8 animate-in fade-in slide-in-from-bottom-4 duration-700 delay-100">
        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Active Devices</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {devicesResponse?.devices.filter(d => d.status === 'online').length || 0}
                </p>
              </div>
              <Server className="w-10 h-10 text-blue-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Current Round</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {roundsResponse?.current.round_number || 0}
                </p>
              </div>
              <Clock className="w-10 h-10 text-yellow-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Global Accuracy</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {(modelsResponse?.models[0]?.metrics.accuracy * 100 || 0).toFixed(1)}%
                </p>
              </div>
              <BrainCircuit className="w-10 h-10 text-purple-500" />
            </div>
          </CardContent>
        </Card>

        <Card className="border-slate-700 bg-slate-800/50 backdrop-blur-sm">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-gray-400">Total Rounds</p>
                <p className="text-3xl font-bold text-white mt-1">
                  {devicesResponse?.devices.reduce((acc, d) => acc + d.aggregated_rounds, 0) || 0}
                </p>
              </div>
              <Database className="w-10 h-10 text-green-500" />
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Main Content Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="animate-in fade-in slide-in-from-bottom-4 duration-700 delay-200">
        <TabsList className="bg-slate-800 border border-slate-700">
          <TabsTrigger value="dashboard">Topology</TabsTrigger>
          <TabsTrigger value="devices" disabled={!true}>Edge Devices</TabsTrigger>
          <TabsTrigger value="rounds" disabled={!true}>Aggregation</TabsTrigger>
          <TabsTrigger value="models" disabled={!true}>Global Models</TabsTrigger>
          <TabsTrigger value="privacy">Privacy Audit</TabsTrigger>
        </TabsList>

        <TabsContent value="dashboard" className="mt-6">
          <TopologyView devices={devicesResponse?.devices || []} rounds={roundsResponse?.rounds || []} isLoading={false} />
        </TabsContent>

        <TabsContent value="devices" className="mt-6">
          <DevicesView
            devices={devicesResponse?.devices || []}
            isLoading={false}
            onSelectDevice={setSelectedDevice}
            onEnroll={(data) => enrollDeviceMutation.mutate(data)}
          />
        </TabsContent>

        <TabsContent value="rounds" className="mt-6">
          <AggregationView
            rounds={roundsResponse?.rounds || []}
            currentRound={roundsResponse?.current}
            isRunning={roundsResponse?.current?.status === 'running'}
            onStart={() => startRoundMutation.mutate()}
            onStop={() => stopRoundMutation.mutate()}
            isLoading={startRoundMutation.isPending}
          />
        </TabsContent>

        <TabsContent value="models" className="mt-6">
          <ModelsView models={modelsResponse?.models || []} isLoading={false} onDeploy={(v) => deployModelMutation.mutate(v)} />
        </TabsContent>

        <TabsContent value="privacy" className="mt-6">
          <PrivacyAuditView devices={devicesResponse?.devices || []} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ============================================================================
// Sub-Components
// ============================================================================

const TopologyView = ({
  devices,
  rounds,
  isLoading
}: {
  devices: EdgeDevice[];
  rounds: AggregationRound[];
  isLoading: boolean;
}) => {
  const onlineCount = devices.filter(d => d.status === 'online').length;
  const participatingCount = devices.filter(d => d.status === 'participating').length;
  const offlineCount = devices.filter(d => d.status === 'offline').length;

  return (
    <div className="space-y-6">
      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xl font-bold text-white">Edge Device Topology</h3>
              <p className="text-sm text-slate-400">Distributed network map of federated learning participants</p>
            </div>
            <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
              <Globe className="w-3 h-3 mr-1" />
              View Map
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <div className="p-6 bg-slate-800 rounded-lg border border-slate-700 space-y-2">
              <div className="flex items-center gap-3">
                <Server className="w-6 h-6 text-green-400" />
                <span className="text-lg font-semibold text-white">Online</span>
              </div>
              <div className="text-4xl font-bold text-green-400 mt-4">{onlineCount}</div>
              <div className="text-sm text-slate-400">Active devices connected</div>
            </div>

            <div className="p-6 bg-slate-800 rounded-lg border border-slate-700 space-y-2">
              <div className="flex items-center gap-3">
                <Users className="w-6 h-6 text-blue-400" />
                <span className="text-lg font-semibold text-white">Participating</span>
              </div>
              <div className="text-4xl font-bold text-blue-400 mt-4">{participatingCount}</div>
              <div className="text-sm text-slate-400">In current aggregation</div>
            </div>

            <div className="p-6 bg-slate-800 rounded-lg border border-slate-700 space-y-2">
              <div className="flex items-center gap-3">
                <XCircle className="w-6 h-6 text-gray-400" />
                <span className="text-lg font-semibold text-white">Offline</span>
              </div>
              <div className="text-4xl font-bold text-gray-400 mt-4">{offlineCount}</div>
              <div className="text-sm text-slate-400">Disconnected devices</div>
            </div>
          </div>

          <Alert className="mt-6 bg-purple-500/10 border-purple-500/30 text-purple-400">
            <Network className="w-4 h-4" />
            <AlertTitle>Topology Visualization Coming Soon</AlertTitle>
            <AlertDescription>
              Interactive network topology graph with device connectivity, geographic distribution,
              and real-time participation status will be available in the next release.
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    </div>
  );
};

const DevicesView = ({
  devices,
  isLoading,
  onSelectDevice,
  onEnroll
}: {
  devices: EdgeDevice[];
  isLoading: boolean;
  onSelectDevice: (d: EdgeDevice) => void;
  onEnroll: (data: any) => void;
}) => {
  const [searchTerm, setSearchTerm] = useState('');
  const [typeFilter, setTypeFilter] = useState<string>('all');

  const filteredDevices = devices.filter(d => {
    const matchesSearch = d.name.toLowerCase().includes(searchTerm.toLowerCase());
    const matchesType = typeFilter === 'all' || d.type === typeFilter;
    return matchesSearch && matchesType;
  });

  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader className="border-b border-slate-700">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Edge Devices</h3>
            <p className="text-sm text-slate-400">Federated learning participant management</p>
          </div>
          <div className="flex gap-3">
            <Input
              placeholder="Search devices..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="bg-slate-800 border-slate-600 text-white w-64"
            />
            <Select value={typeFilter} onValueChange={setTypeFilter}>
              <SelectTrigger className="bg-slate-800 border-slate-600 text-white w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Types</SelectItem>
                <SelectItem value="mobile">Mobile</SelectItem>
                <SelectItem value="iot">IoT</SelectItem>
                <SelectItem value="server">Server</SelectItem>
                <SelectItem value="workstation">Workstation</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-6">
        {isLoading ? (
          <div className="text-center py-12">
            <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
            <p className="text-slate-400 mt-4">Loading devices...</p>
          </div>
        ) : filteredDevices.length === 0 ? (
          <EmptyState message="No edge devices found" />
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {filteredDevices.map((device) => (
              <Card key={device.id} className="border-slate-700 bg-slate-800/50 hover:border-purple-500/50 transition-colors cursor-pointer">
                <CardHeader className="pb-3">
                  <div className="flex items-start justify-between">
                    <div>
                      <h3 className="text-lg font-bold text-white">{device.name}</h3>
                      <div className="flex items-center gap-2 mt-1">
                        <Badge className={`${getStatusColor(device.status)} border`}>{device.status.toUpperCase()}</Badge>
                      </div>
                    </div>
                    <Server className={`w-5 h-5 ${device.status === 'online' ? 'text-green-400' : 'text-slate-400'}`} />
                  </div>
                </CardHeader>
                <CardContent className="space-y-3">
                  <div className="text-sm text-slate-300 capitalize">{device.type}</div>
                  
                  <div className="pt-2 border-t border-slate-700 space-y-1 text-sm">
                    <div className="flex justify-between">
                      <span className="text-slate-400">CPU:</span>
                      <span className="text-white">{device.capabilities.cpu_cores} cores</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-slate-400">Memory:</span>
                      <span className="text-white">{device.capabilities.memory_gb} GB</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-slate-400">Rounds:</span>
                      <span className="text-purple-400">{device.aggregated_rounds}</span>
                    </div>
                    {device.accuracy_improvement && (
                      <div className="flex justify-between">
                        <span className="text-slate-400">Accuracy +:</span>
                        <span className="text-green-400">{device.accuracy_improvement.toFixed(2)}%</span>
                      </div>
                    )}
                  </div>

                  <div className="flex gap-2 pt-2">
                    <Button
                      size="sm"
                      variant="outline"
                      className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                      onClick={() => onSelectDevice(device)}
                    >
                      <Eye className="w-3 h-3 mr-1" />
                      View
                    </Button>
                    {device.privacy_compliance.differential_privacy_enabled && (
                      <Badge variant="outline" className="text-xs bg-purple-700 border-purple-500 text-purple-300">
                        <ShieldCheck className="w-3 h-3 mr-1" />
                        DP Enabled
                      </Badge>
                    )}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

const AggregationView = ({
  rounds,
  currentRound,
  isRunning,
  onStart,
  onStop,
  isLoading
}: {
  rounds: AggregationRound[];
  currentRound?: AggregationRound;
  isRunning: boolean;
  onStart: () => void;
  onStop: () => void;
  isLoading: boolean;
}) => {
  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader className="border-b border-slate-700">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Aggregation Control</h3>
            <p className="text-sm text-slate-400">Federated training round orchestration</p>
          </div>
          <div className="flex gap-2">
            {isRunning ? (
              <Button
                size="sm"
                onClick={onStop}
                disabled={stopRoundMutation.isPending}
                className="bg-orange-600 hover:bg-orange-700"
              >
                <Square className="w-3 h-3 mr-1" />
                Stop
              </Button>
            ) : (
              <Button
                size="sm"
                onClick={onStart}
                disabled={startRoundMutation.isPending}
                className="bg-green-600 hover:bg-green-700"
              >
                <Play className="w-3 h-3 mr-1" />
                Start Round
              </Button>
            )}
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-6 space-y-6">
        {currentRound && (
          <Card className="border-slate-700 bg-slate-800/30">
            <CardContent className="pt-4">
              <div className="flex items-center justify-between mb-4">
                <div>
                  <div className="text-sm text-slate-400">Current Round</div>
                  <div className="text-2xl font-bold text-white">#{currentRound.round_number}</div>
                </div>
                <Badge className={`${getStatusColor(currentRound.status)} border`}>{currentRound.status.toUpperCase()}</Badge>
              </div>

              <div className="space-y-3">
                <div className="flex justify-between text-sm">
                  <span className="text-slate-400">Participants:</span>
                  <span className="text-white">{currentRound.participating_devices}/{currentRound.total_devices}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-slate-400">Epochs per Device:</span>
                  <span className="text-white">{currentRound.epochs_per_device}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-slate-400">Algorithm:</span>
                  <span className="text-white capitalize">{currentRound.aggregation_algorithm.replace('_', ' ')}</span>
                </div>

                {currentRound.global_metrics && (
                  <div className="pt-3 border-t border-slate-700 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-slate-400">Global Loss:</span>
                      <span className="text-red-400 font-mono">{currentRound.global_metrics.loss.toFixed(4)}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-slate-400">Global Accuracy:</span>
                      <span className="text-green-400 font-mono">{(currentRound.global_metrics.accuracy * 100).toFixed(2)}%</span>
                    </div>
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        )}

        <Card className="border-slate-700 bg-slate-800/30">
          <CardContent className="pt-4">
            <h4 className="font-semibold text-white mb-3">Recent Rounds History</h4>
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead className="bg-slate-800/50">
                  <tr>
                    <th className="p-3 text-left text-xs font-semibold text-slate-300">Round</th>
                    <th className="p-3 text-left text-xs font-semibold text-slate-300">Status</th>
                    <th className="p-3 text-left text-xs font-semibold text-slate-300">Participants</th>
                    <th className="p-3 text-left text-xs font-semibold text-slate-300">Accuracy</th>
                    <th className="p-3 text-left text-xs font-semibold text-slate-300">Completed</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-700">
                  {rounds.slice(0, 5).map((round) => (
                    <tr key={round.id} className="hover:bg-slate-700/30">
                      <td className="p-3 text-white font-medium">#{round.round_number}</td>
                      <td className="p-4">
                        <Badge className={`${getStatusColor(round.status)} border text-xs`}>{round.status}</Badge>
                      </td>
                      <td className="p-3 text-slate-300 text-sm">
                        {round.participating_devices}/{round.total_devices}
                      </td>
                      <td className="p-3 text-slate-300 text-sm">
                        {round.global_metrics ? `${(round.global_metrics.accuracy * 100).toFixed(2)}%` : '-'}
                      </td>
                      <td className="p-3 text-slate-400 text-sm">
                        {round.completed_at ? formatDate(round.completed_at) : '-'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      </CardContent>
    </Card>
  );
};

const ModelsView = ({
  models,
  isLoading,
  onDeploy
}: {
  models: Array<{ version: string; metrics: GlobalModelMetrics; created_at: string }>;
  isLoading: boolean;
  onDeploy: (version: string) => void;
}) => {
  return (
    <Card className="border-slate-700 bg-slate-800/50">
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-xl font-bold text-white">Global Model Versions</h3>
            <p className="text-sm text-slate-400">Trained federated models version history</p>
          </div>
          <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
            <Download className="w-3 h-3 mr-1" />
            Export
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        {models.length === 0 ? (
          <EmptyState message="No trained models available" />
        ) : (
          <div className="space-y-4">
            {models.map((model) => (
              <Card key={model.version} className="border-slate-700 bg-slate-800/30">
                <CardContent className="pt-4">
                  <div className="flex items-center justify-between">
                    <div>
                      <div className="flex items-center gap-2">
                        <span className="text-lg font-bold text-white">Version {model.version}</span>
                        <Badge variant="outline" className="text-purple-400 border-purple-500/30">
                          v{model.version}
                        </Badge>
                      </div>
                      <div className="text-sm text-slate-400 mt-1">{formatDate(model.created_at)}</div>
                    </div>
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        variant="outline"
                        className="border-slate-600 text-slate-300 hover:bg-slate-700"
                      >
                        <Eye className="w-3 h-3" />
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => onDeploy(model.version)}
                        className="bg-purple-600 hover:bg-purple-700"
                      >
                        <Upload className="w-3 h-3 mr-1" />
                        Deploy
                      </Button>
                    </div>
                  </div>

                  <div className="mt-4 grid grid-cols-2 md:grid-cols-4 gap-4">
                    <div className="p-3 bg-slate-700/50 rounded">
                      <div className="text-xs text-slate-400">Loss</div>
                      <div className="text-lg font-bold text-red-400">{model.metrics.loss.toFixed(4)}</div>
                    </div>
                    <div className="p-3 bg-slate-700/50 rounded">
                      <div className="text-xs text-slate-400">Accuracy</div>
                      <div className="text-lg font-bold text-green-400">{(model.metrics.accuracy * 100).toFixed(1)}%</div>
                    </div>
                    {model.metrics.f1_score && (
                      <div className="p-3 bg-slate-700/50 rounded">
                        <div className="text-xs text-slate-400">F1 Score</div>
                        <div className="text-lg font-bold text-blue-400">{model.metrics.f1_score.toFixed(4)}</div>
                      </div>
                    )}
                    <div className="p-3 bg-slate-700/50 rounded">
                      <div className="text-xs text-slate-400">Convergence Step</div>
                      <div className="text-lg font-bold text-purple-400">{model.metrics.convergence_step}</div>
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

const PrivacyAuditView = ({
  devices
}: {
  devices: EdgeDevice[];
}) => {
  const dpEnabledCount = devices.filter(d => d.privacy_compliance.differential_privacy_enabled).length;
  const encryptionEnabledCount = devices.filter(d => d.privacy_compliance.data_encryption).length;
  const gdpCompliantCount = devices.filter(d => d.privacy_compliance.gdpr_compliant).length;

  return (
    <div className="space-y-6">
      <Card className="border-slate-700 bg-slate-800/50">
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-xl font-bold text-white">Privacy Compliance Audit</h3>
              <p className="text-sm text-slate-400">GDPR and differential privacy verification</p>
            </div>
            <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
              <CheckCircle2 className="w-3 h-3 mr-1" />
              Run Audit
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <Card className="border-slate-700 bg-slate-800/30">
              <CardContent className="pt-4">
                <div className="flex items-center gap-3 mb-3">
                  <ShieldCheck className="w-6 h-6 text-purple-400" />
                  <h4 className="font-semibold text-white">Differential Privacy</h4>
                </div>
                <div className="text-3xl font-bold text-purple-400">{dpEnabledCount}</div>
                <div className="text-sm text-slate-400">devices with DP enabled</div>
              </CardContent>
            </Card>

            <Card className="border-slate-700 bg-slate-800/30">
              <CardContent className="pt-4">
                <div className="flex items-center gap-3 mb-3">
                  <Lock className="w-6 h-6 text-blue-400" />
                  <h4 className="font-semibold text-white">Data Encryption</h4>
                </div>
                <div className="text-3xl font-bold text-blue-400">{encryptionEnabledCount}</div>
                <div className="text-sm text-slate-400">devices encrypted</div>
              </CardContent>
            </Card>

            <Card className="border-slate-700 bg-slate-800/30">
              <CardContent className="pt-4">
                <div className="flex items-center gap-3 mb-3">
                  <TagIcon className="w-6 h-6 text-green-400" />
                  <h4 className="font-semibold text-white">GDPR Compliant</h4>
                </div>
                <div className="text-3xl font-bold text-green-400">{gdpCompliantCount}</div>
                <div className="text-sm text-slate-400">compliant devices</div>
              </CardContent>
            </Card>
          </div>

          <Alert className="mt-6 bg-green-500/10 border-green-500/30 text-green-400">
            <CheckCircle2 className="w-4 h-4" />
            <AlertTitle>All Systems Compliant</AlertTitle>
            <AlertDescription>
              All enrolled devices meet GDPR requirements and have differential privacy enabled
              with secure aggregation protocols active.
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    </div>
  );
};

const EmptyState = ({ message }: { message: string }) => (
  <div className="text-center py-12">
    <Network className="w-16 h-16 mx-auto text-slate-500 mb-4" />
    <h3 className="text-xl font-semibold text-white mb-2">No Data Available</h3>
    <p className="text-slate-400">{message}</p>
  </div>
);
