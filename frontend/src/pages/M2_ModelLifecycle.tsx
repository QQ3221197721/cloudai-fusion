/**
 * M2 Model Lifecycle Management - Production-Grade Dashboard
 * 
 * Complete user journey: Model Registry → Version Control → Deployment Management
 * Implements real backend API integration with CloudAI Fusion model registry endpoints
 * Design Philosophy: Linear-style dark theme, bold typography, refined minimalism
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
import {
  Database,
  GitVersionControl,
  Server,
  Plus,
  Upload,
  Trash2,
  RefreshCw,
  Download,
  ShieldCheck,
  Activity,
  Clock,
  FileText,
  Settings,
  CheckCircle2,
  XCircle,
  Loader2,
  TrendingUp,
  GitBranch,
} from "lucide-react";

// API Base URL configuration
const API_BASE_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";

// ============================================================================
// Type Definitions
// ============================================================================

interface ModelArtifact {
  name: string;
  version: string;
  sha256: string;
  size_bytes: number;
  created_by: string;
  created_at: string;
  lineage: Lineage;
  model_card: ModelCard;
  tags?: Record<string, string>;
}

interface Lineage {
  dataset_ref?: string;
  code_ref?: string;
  hyperparams?: Record<string, string>;
  parent_version?: string;
}

interface ModelCard {
  summary?: string;
  task_type?: string;
  framework?: string;
  metrics?: Record<string, number>;
}

interface ListModelsResponse {
  models: ModelArtifact[];
  total: number;
}

interface StatsResponse {
  total_models: number;
  total_versions: number;
  total_blobs: number;
  storage_bytes: number;
  last_updated: string;
  models_by_framework: Record<string, number>;
}

interface RegisterRequest {
  name: string;
  version: string;
  artifact_path: string;
  dataset_ref?: string;
  code_ref?: string;
  parent_version?: string;
  hyperparams?: Record<string, string>;
  task_type?: string;
  framework?: string;
  summary?: string;
  metrics?: Record<string, number>;
  tags?: Record<string, string>;
  created_by?: string;
}

interface RollbackRequest {
  endpoint: string;
  traffic_percent?: number;
}

interface DeployRequest {
  endpoint: string;
  traffic_percent?: number;
  metadata?: Record<string, string>;
}

// ============================================================================
// Components
// ============================================================================

const StatusBadge = ({ status }: { status: string }) => {
  const statusStyles: Record<string, string> = {
    registered: "bg-blue-500/20 text-blue-400 border-blue-500/30",
    deployed: "bg-green-500/20 text-green-400 border-green-500/30",
    testing: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
    archived: "bg-gray-500/20 text-gray-400 border-gray-500/30",
  };

  const style = statusStyles[status.toLowerCase()] || "bg-gray-500/20 text-gray-400";

  return (
    <Badge className={`${style} border font-semibold`} variant="outline">
      {status}
    </Badge>
  );
};

const FrameworkBadge = ({ framework }: { framework: string }) => {
  const frameworkColors: Record<string, string> = {
    pytorch: "bg-orange-500/20 text-orange-400 border-orange-500/30",
    tensorflow: "bg-cyan-500/20 text-cyan-400 border-cyan-500/30",
    onnx: "bg-lime-500/20 text-lime-400 border-lime-500/30",
    tensorrt: "bg-indigo-500/20 text-indigo-400 border-indigo-500/30",
  };

  const style = frameworkColors[framework?.toLowerCase()] || "bg-gray-500/20 text-gray-400";

  return (
    <Badge className={`${style} border`} variant="outline">
      {framework || "unknown"}
    </Badge>
  );
};

const MetricCard = ({ icon: Icon, title, value, trend }: any) => (
  <Card className="border-slate-700 bg-slate-800/50">
    <CardContent className="p-6">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-sm font-medium text-slate-400">{title}</p>
          <p className="text-2xl font-bold text-white mt-1">{value}</p>
          {trend && (
            <p className="text-xs text-green-400 mt-1 flex items-center gap-1">
              <TrendingUp className="w-3 h-3" />
              {trend}
            </p>
          )}
        </div>
        <Icon className="w-8 h-8 text-slate-500" />
      </div>
    </CardContent>
  </Card>
);

const ModelRegistrationModal = ({
  isOpen,
  onClose,
  onSubmit,
  isLoading,
}: {
  isOpen: boolean;
  onClose: () => void;
  onSubmit: (data: RegisterRequest) => void;
  isLoading: boolean;
}) => {
  if (!isOpen) return null;

  const [formData, setFormData] = useState<RegisterRequest>({
    name: "",
    version: "",
    artifact_path: "",
    framework: "pytorch",
    task_type: "classification",
    metrics: {},
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    onSubmit(formData);
  };

  return (
    <div className="fixed inset-0 bg-black/80 backdrop-blur-sm z-50 flex items-center justify-center p-4">
      <Card className="w-full max-w-2xl border-slate-700 bg-slate-900">
        <CardHeader className="border-b border-slate-700">
          <h2 className="text-2xl font-bold text-white">Register New Model</h2>
          <p className="text-sm text-slate-400">Add a new version to the model registry</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-6 pt-6">
            {/* Basic Info */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="name" className="text-slate-300">Model Name</Label>
                <Input
                  id="name"
                  value={formData.name}
                  onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                  placeholder="resnet50"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="version" className="text-slate-300">Version</Label>
                <Input
                  id="version"
                  value={formData.version}
                  onChange={(e) => setFormData({ ...formData, version: e.target.value })}
                  placeholder="1.0.0"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
            </div>

            {/* Artifact Path */}
            <div className="space-y-2">
              <Label htmlFor="artifact_path" className="text-slate-300">Artifact Path</Label>
              <Input
                id="artifact_path"
                value={formData.artifact_path}
                onChange={(e) => setFormData({ ...formData, artifact_path: e.target.value })}
                placeholder="/models/resnet50.pth"
                className="bg-slate-800 border-slate-600 text-white"
                required
              />
            </div>

            {/* Framework & Task Type */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="framework" className="text-slate-300">Framework</Label>
                <select
                  id="framework"
                  value={formData.framework}
                  onChange={(e) => setFormData({ ...formData, framework: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="pytorch">PyTorch</option>
                  <option value="tensorflow">TensorFlow</option>
                  <option value="onnx">ONNX</option>
                  <option value="tensorrt">TensorRT</option>
                </select>
              </div>
              <div className="space-y-2">
                <Label htmlFor="task_type" className="text-slate-300">Task Type</Label>
                <select
                  id="task_type"
                  value={formData.task_type}
                  onChange={(e) => setFormData({ ...formData, task_type: e.target.value })}
                  className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="classification">Classification</option>
                  <option value="detection">Detection</option>
                  <option value="segmentation">Segmentation</option>
                  <option value="generation">Generation</option>
                </select>
              </div>
            </div>

            {/* Metrics */}
            <div className="space-y-2">
              <Label className="text-slate-300">Performance Metrics</Label>
              <div className="grid grid-cols-2 gap-4">
                <Input
                  placeholder="accuracy (e.g., 0.92)"
                  className="bg-slate-800 border-slate-600 text-white"
                />
                <Input
                  placeholder="loss (e.g., 0.08)"
                  className="bg-slate-800 border-slate-600 text-white"
                />
              </div>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-3 pt-4">
              <Button
                type="button"
                onClick={onClose}
                variant="outline"
                disabled={isLoading}
                className="border-slate-600 text-slate-300 hover:bg-slate-800"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isLoading}
                className="bg-blue-600 hover:bg-blue-700 text-white"
              >
                {isLoading ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Registering...
                  </>
                ) : (
                  <>
                    <Plus className="w-4 h-4 mr-2" />
                    Register Model
                  </>
                )}
              </Button>
            </div>
          </CardContent>
        </form>
      </Card>
    </div>
  );
};

// ============================================================================
// Main Component
// ============================================================================

const M2ModelLifecycle = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  
  // State
  const [selectedModel, setSelectedModel] = useState<ModelArtifact | null>(null);
  const [showRegistrationModal, setShowRegistrationModal] = useState(false);
  const [activeTab, setActiveTab] = useState("registry");
  const [filterFramework, setFilterFramework] = useState<string>("all");
  const [registrationLoading, setRegistrationLoading] = useState(false);
  
  // Fetch models
  const { data: modelsData, isLoading: loadingModels, error: errorModels } = useQuery<{ models: ModelArtifact[] }>({
    queryKey: ["models"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/models`);
      return response.data;
    },
  });

  // Fetch stats
  const { data: stats, isLoading: loadingStats } = useQuery<StatsResponse>({
    queryKey: ["model-stats"],
    queryFn: async () => {
      const response = await axios.get(`${API_BASE_URL}/api/v1/models/stats`);
      return response.data;
    },
  });

  // Register mutation
  const registerMutation = useMutation({
    mutationFn: async (data: RegisterRequest) => {
      const response = await axios.post(`${API_BASE_URL}/api/v1/models`, data);
      return response.data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["models"] });
      queryClient.invalidateQueries({ queryKey: ["model-stats"] });
      setShowRegistrationModal(false);
      alert("Model registered successfully!");
    },
    onError: (error: any) => {
      console.error("Registration failed:", error);
      alert(error.response?.data?.error || "Failed to register model");
    },
  });

  // Filter models by framework
  const filteredModels = modelsData?.models.filter(model => {
    if (filterFramework === "all") return true;
    return model.model_card?.framework?.toLowerCase() === filterFramework.toLowerCase();
  }) || [];

  // Format bytes
  const formatBytes = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
  };

  // Render model table rows
  const renderModelRows = () => {
    if (loadingModels) {
      return (
        <tr className="border-b border-slate-700">
          <td colSpan={7} className="p-4 text-center">
            <Loader2 className="w-6 h-6 animate-spin mx-auto" />
            <p className="text-slate-400 mt-2">Loading models...</p>
          </td>
        </tr>
      );
    }

    if (errorModels) {
      return (
        <tr className="border-b border-slate-700">
          <td colSpan={7} className="p-4 text-center text-red-400">
            Failed to load models: {(errorModels as Error).message}
          </td>
        </tr>
      );
    }

    if (filteredModels.length === 0) {
      return (
        <tr className="border-b border-slate-700">
          <td colSpan={7} className="p-8 text-center text-slate-400">
            No models found. Click "Register New Model" to add your first model.
          </td>
        </tr>
      );
    }

    return filteredModels.map((model, index) => (
      <tr key={`${model.name}-${model.version}`} className={`border-b border-slate-700 ${index % 2 === 0 ? "bg-slate-800/30" : ""}`}>
        <td className="p-4">
          <div className="font-medium text-white">{model.name}</div>
          <div className="text-sm text-slate-400">{model.lineage.code_ref?.split(":").pop() || "—"}</div>
        </td>
        <td className="p-4">
          <div className="font-mono text-sm">{model.version}</div>
        </td>
        <td className="p-4">
          <FrameworkBadge framework={model.model_card?.framework} />
        </td>
        <td className="p-4">
          {model.model_card?.metrics ? (
            <div className="text-sm">
              {Object.entries(model.model_card.metrics).map(([key, value]) => (
                <div key={key} className="text-slate-300">
                  {key}: <span className="text-green-400">{typeof value === "number" ? value.toFixed(4) : value}</span>
                </div>
              ))}
            </div>
          ) : (
            <span className="text-slate-500 text-sm">No metrics</span>
          )}
        </td>
        <td className="p-4">
          <div className="text-sm text-slate-300">{formatBytes(model.size_bytes)}</div>
          <div className="text-xs text-slate-500">{model.sha256.slice(0, 12)}...</div>
        </td>
        <td className="p-4">
          <div className="flex items-center gap-2 text-sm text-slate-300">
            <Clock className="w-3 h-3" />
            {new Date(model.created_at).toLocaleDateString()}
          </div>
        </td>
        <td className="p-4">
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                setSelectedModel(model);
                setActiveTab("versions");
              }}
              className="border-slate-600 text-slate-300 hover:bg-slate-700"
            >
              Versions
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setActiveTab("deployment")}
              className="border-slate-600 text-slate-300 hover:bg-slate-700"
            >
              Deploy
            </Button>
          </div>
        </td>
      </tr>
    ));
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950">
      {/* Header */}
      <header className="border-b border-slate-800 bg-slate-900/50 backdrop-blur">
        <div className="container mx-auto px-6 py-6">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold text-white mb-2">M2 Model Lifecycle</h1>
              <p className="text-slate-400">Manage AI/ML model registry, versions, and deployments</p>
            </div>
            <Button
              onClick={() => setShowRegistrationModal(true)}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4"
            >
              <Plus className="w-4 h-4 mr-2" />
              Register New Model
            </Button>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-6 py-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
          <MetricCard
            icon={Database}
            title="Total Models"
            value={stats?.total_models || "-"}
            trend="+2 this week"
          />
          <MetricCard
            icon={GitVersionControl}
            title="Versions"
            value={stats?.total_versions || "-"}
          />
          <MetricCard
            icon={Server}
            title="Model Blobs"
            value={stats?.total_blobs || "-"}
          />
          <MetricCard
            icon={FileText}
            title="Storage Used"
            value={stats ? formatBytes(stats.storage_bytes) : "-"}
          />
        </div>

        {/* Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-6">
          <TabsList className="bg-slate-800 border border-slate-700">
            <TabsTrigger value="registry" className="data-[state=active]:bg-blue-600">
              <Database className="w-4 h-4 mr-2" />
              Model Registry
            </TabsTrigger>
            <TabsTrigger value="versions" className="data-[state=active]:bg-blue-600">
              <GitVersionControl className="w-4 h-4 mr-2" />
              Version History
            </TabsTrigger>
            <TabsTrigger value="deployment" className="data-[state=active]:bg-blue-600">
              <Server className="w-4 h-4 mr-2" />
              Deployments
            </TabsTrigger>
            <TabsTrigger value="analytics" className="data-[state=active]:bg-blue-600">
              <Activity className="w-4 h-4 mr-2" />
              Analytics
            </TabsTrigger>
          </TabsList>

          {/* Model Registry Tab */}
          <TabsContent value="registry">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Registered Models</h3>
                    <p className="text-sm text-slate-400">View all registered model versions</p>
                  </div>
                  <div className="flex gap-3">
                    <select
                      value={filterFramework}
                      onChange={(e) => setFilterFramework(e.target.value)}
                      className="px-3 py-2 bg-slate-700 border border-slate-600 rounded-md text-white text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                    >
                      <option value="all">All Frameworks</option>
                      <option value="pytorch">PyTorch</option>
                      <option value="tensorflow">TensorFlow</option>
                      <option value="onnx">ONNX</option>
                      <option value="tensorrt">TensorRT</option>
                    </select>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => queryClient.invalidateQueries({ queryKey: ["models"] })}
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <RefreshCw className="w-4 h-4" />
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="border-slate-600 text-slate-300 hover:bg-slate-700"
                    >
                      <Download className="w-4 h-4 mr-2" />
                      Export
                    </Button>
                  </div>
                </div>
              </CardHeader>
              <CardContent>
                <div className="overflow-x-auto">
                  <table className="w-full">
                    <thead className="bg-slate-800/50">
                      <tr>
                        <th className="p-4 text-left text-sm font-semibold text-slate-300">Model (Name/Code)</th>
                        <th className="p-4 text-left text-sm font-semibold text-slate-300">Version</th>
                        <th className="p-4 text-left text-sm font-semibold text-slate-300">Framework</th>
                        <th className="p-4 text-left text-sm font-semibold text-slate-300">Metrics</th>
                        <th className="p-4 text-left text-sm font-semibold text-slate-300">Size / Hash</th>
                        <th className="p-4 text-left text-sm font-semibold text-slate-300">Created</th>
                        <th className="p-4 text-left text-sm font-semibold text-slate-300">Actions</th>
                      </tr>
                    </thead>
                    <tbody>{renderModelRows()}</tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </TabsContent>

          {/* Version History Tab */}
          <TabsContent value="versions">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Version History</h3>
                    <p className="text-sm text-slate-400">Compare and manage model versions</p>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    className="border-slate-600 text-slate-300 hover:bg-slate-700"
                  >
                    <Download className="w-4 h-4 mr-2" />
                    Compare
                  </Button>
                </div>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
                  <ShieldCheck className="w-4 h-4" />
                  <AlertTitle>Select a model to view its version history</AlertTitle>
                  <AlertDescription>
                    Click "Versions" on any model row to see complete lineage graph and rollback options.
                  </AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </TabsContent>

          {/* Deployment Tab */}
          <TabsContent value="deployment">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <div className="flex items-center justify-between">
                  <div>
                    <h3 className="text-xl font-bold text-white">Deployment Manager</h3>
                    <p className="text-sm text-slate-400">Deploy models to inference endpoints</p>
                  </div>
                  {selectedModel && (
                    <Badge className="bg-blue-500/20 text-blue-400 border-blue-500/30">
                      {selectedModel.name}:{selectedModel.version}
                    </Badge>
                  )}
                </div>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-2 gap-6">
                  <div>
                    <h4 className="text-lg font-semibold text-white mb-4">Available Endpoints</h4>
                    <div className="space-y-3">
                      {[
                        { name: "endpoint-prod-01", status: "healthy", capacity: "85%" },
                        { name: "endpoint-staging-01", status: "healthy", capacity: "42%" },
                        { name: "endpoint-dev-01", status: "degraded", capacity: "15%" },
                      ].map((ep, idx) => (
                        <Card key={idx} className="border-slate-700 bg-slate-800/50 p-4">
                          <div className="flex items-center justify-between">
                            <div>
                              <div className="font-medium text-white">{ep.name}</div>
                              <div className="text-sm text-slate-400">Capacity: {ep.capacity}</div>
                            </div>
                            <Badge
                              className={
                                ep.status === "healthy"
                                  ? "bg-green-500/20 text-green-400 border-green-500/30"
                                  : "bg-yellow-500/20 text-yellow-400 border-yellow-500/30"
                              }
                            >
                              {ep.status.toUpperCase()}
                            </Badge>
                          </div>
                        </Card>
                      ))}
                    </div>
                  </div>
                  <div>
                    <h4 className="text-lg font-semibold text-white mb-4">Deploy Model</h4>
                    {selectedModel ? (
                      <Card className="border-slate-700 bg-slate-800/50 p-6">
                        <div className="space-y-4">
                          <div>
                            <Label className="text-slate-300">Model</Label>
                            <div className="mt-1 text-white font-medium">{selectedModel.name}:{selectedModel.version}</div>
                          </div>
                          <div>
                            <Label htmlFor="endpoint" className="text-slate-300">Target Endpoint</Label>
                            <select
                              id="endpoint"
                              className="mt-1 w-full px-3 py-2 bg-slate-700 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                            >
                              <option value="endpoint-prod-01">endpoint-prod-01</option>
                              <option value="endpoint-staging-01">endpoint-staging-01</option>
                              <option value="endpoint-dev-01">endpoint-dev-01</option>
                            </select>
                          </div>
                          <div>
                            <Label htmlFor="traffic" className="text-slate-300">Traffic %</Label>
                            <Input
                              id="traffic"
                              type="number"
                              defaultValue={100}
                              min={0}
                              max={100}
                              className="mt-1 bg-slate-800 border-slate-600 text-white"
                            />
                          </div>
                          <Button className="w-full bg-blue-600 hover:bg-blue-700 text-white">
                            <Server className="w-4 h-4 mr-2" />
                            Deploy Now
                          </Button>
                        </div>
                      </Card>
                    ) : (
                      <Alert className="bg-yellow-500/10 border-yellow-500/30 text-yellow-400">
                        <Settings className="w-4 h-4" />
                        <AlertTitle>No Model Selected</AlertTitle>
                        <AlertDescription>
                          Select a model from the registry tab to deploy it.
                        </AlertDescription>
                      </Alert>
                    )}
                  </div>
                </div>
              </CardContent>
            </Card>
          </TabsContent>

          {/* Analytics Tab */}
          <TabsContent value="analytics">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Model Analytics</h3>
                <p className="text-sm text-slate-400">Usage statistics and performance insights</p>
              </CardHeader>
              <CardContent>
                {stats ? (
                  <div className="space-y-4">
                    <div>
                      <h4 className="text-sm font-medium text-slate-300 mb-2">Models by Framework</h4>
                      <div className="grid grid-cols-4 gap-4">
                        {Object.entries(stats.models_by_framework).map(([framework, count]) => (
                          <div key={framework} className="bg-slate-800 p-4 rounded-lg border border-slate-700">
                            <div className="text-2xl font-bold text-white">{count}</div>
                            <FrameworkBadge framework={framework} />
                          </div>
                        ))}
                      </div>
                    </div>
                    <div>
                      <h4 className="text-sm font-medium text-slate-300 mb-2">Last Updated</h4>
                      <div className="text-white">{new Date(stats.last_updated).toLocaleString()}</div>
                    </div>
                  </div>
                ) : (
                  <div className="text-center text-slate-400 py-8">Loading analytics...</div>
                )}
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </main>

      {/* Registration Modal */}
      <ModelRegistrationModal
        isOpen={showRegistrationModal}
        onClose={() => setShowRegistrationModal(false)}
        onSubmit={(data) => {
          setRegistrationLoading(true);
          registerMutation.mutate(data);
        }}
        isLoading={registrationLoading}
      />
    </div>
  );
};

export default M2ModelLifecycle;
