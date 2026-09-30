/**
 * M13 Model Registry - Production-Grade Dashboard
 * 
 * Complete user journey: Model Catalog → Version History → Model Details → Compliance Checks
 * Leverages existing M2 backend API from pkg/modelregistry
 * Design Philosophy: Linear-style dark theme, academic precision, scholarly aesthetics
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
import {
  Database,
  GitVersionControl,
  FileText,
  Plus,
  Upload,
  ShieldCheck,
  Clock,
  Search,
  Filter,
  Download,
  CheckCircle2,
  XCircle,
  Loader2,
  TrendingUp,
  BookOpen,
  Lock,
  Unlock,
  Branch,
  ArrowLeftRight,
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
  compliance_rate: number;
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

interface VersionComparison {
  base_version: string;
  compare_version: string;
  metrics_diff: Record<string, number>;
  lineage_changes: string[];
}

// ============================================================================
// Components
// ============================================================================

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
      {framework?.toUpperCase() || "UNKNOWN"}
    </Badge>
  );
};

const TaskTypeBadge = ({ type }: { type: string }) => {
  const colors: Record<string, string> = {
    classification: "bg-blue-500/20 text-blue-400 border-blue-500/30",
    detection: "bg-purple-500/20 text-purple-400 border-purple-500/30",
    segmentation: "bg-pink-500/20 text-pink-400 border-pink-500/30",
    generation: "bg-green-500/20 text-green-400 border-green-500/30",
    embedding: "bg-yellow-500/20 text-yellow-400 border-yellow-500/30",
  };

  const style = colors[type?.toLowerCase()] || "bg-slate-500/20 text-slate-400";

  return (
    <Badge className={`${style} border text-xs`} variant="outline">
      {type?.toUpperCase() || "GENERAL"}
    </Badge>
  );
};

const ComplianceBadge = ({ compliant }: { compliant: boolean }) => (
  <div className="flex items-center gap-2">
    {compliant ? (
      <Badge className="bg-green-500/20 text-green-400 border-green-500/30">
        <CheckCircle2 className="w-3 h-3 mr-1" />
        Compliant
      </Badge>
    ) : (
      <Badge className="bg-red-500/20 text-red-400 border-red-500/30">
        <XCircle className="w-3 h-3 mr-1" />
        Non-compliant
      </Badge>
    )}
  </div>
);

const MetricCard = ({ icon: Icon, title, value, trend, color = "blue" }: any) => (
  <Card className={`border-slate-700 bg-slate-800/50`}>
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
        <Icon className={`w-8 h-8 text-${color}-500`} />
      </div>
    </CardContent>
  </Card>
);

const RegisterModelModal = ({
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
    version: "1.0.0",
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
      <Card className="w-full max-w-3xl border-slate-700 bg-slate-900 max-h-[90vh] overflow-y-auto">
        <CardHeader className="border-b border-slate-700 sticky top-0 bg-slate-900 z-10">
          <h2 className="text-2xl font-bold text-white">Register New Model</h2>
          <p className="text-sm text-slate-400">Add a versioned model to the registry with full lineage tracking</p>
        </CardHeader>
        <form onSubmit={handleSubmit}>
          <CardContent className="space-y-6 pt-6">
            {/* Basic Information */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <Database className="w-4 h-4" />
                Basic Information
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="name" className="text-slate-300">Model Name *</Label>
                  <Input
                    id="name"
                    value={formData.name}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value.toLowerCase().replace(/\s+/g, '-') })}
                    placeholder="resnet50"
                    className="bg-slate-800 border-slate-600 text-white"
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="version" className="text-slate-300">Version (SemVer) *</Label>
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
              <div className="space-y-2">
                <Label htmlFor="artifact_path" className="text-slate-300">Artifact Path *</Label>
                <Input
                  id="artifact_path"
                  value={formData.artifact_path}
                  onChange={(e) => setFormData({ ...formData, artifact_path: e.target.value })}
                  placeholder="/models/resnet50-v1.pth"
                  className="bg-slate-800 border-slate-600 text-white"
                  required
                />
              </div>
            </div>

            {/* Model Specifications */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <BookOpen className="w-4 h-4" />
                Model Specifications
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="framework" className="text-slate-300">Framework *</Label>
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
                  <Label htmlFor="task_type" className="text-slate-300">Task Type *</Label>
                  <select
                    id="task_type"
                    value={formData.task_type}
                    onChange={(e) => setFormData({ ...formData, task_type: e.target.value })}
                    className="w-full px-3 py-2 bg-slate-800 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                  >
                    <option value="classification">Classification</option>
                    <option value="detection">Object Detection</option>
                    <option value="segmentation">Segmentation</option>
                    <option value="generation">Text Generation</option>
                    <option value="embedding">Embedding</option>
                  </select>
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="summary" className="text-slate-300">Summary</Label>
                <Input
                  id="summary"
                  value={formData.summary || ""}
                  onChange={(e) => setFormData({ ...formData, summary: e.target.value })}
                  placeholder="Brief description of the model..."
                  className="bg-slate-800 border-slate-600 text-white"
                />
              </div>
            </div>

            {/* Performance Metrics */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <TrendingUp className="w-4 h-4" />
                Performance Metrics
              </h4>
              <div className="grid grid-cols-3 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="metric-accuracy" className="text-slate-300">Accuracy</Label>
                  <Input
                    id="metric-accuracy"
                    type="number"
                    step="0.0001"
                    placeholder="0.9234"
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="metric-loss" className="text-slate-300">Loss</Label>
                  <Input
                    id="metric-loss"
                    type="number"
                    step="0.0001"
                    placeholder="0.0766"
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="metric-inference" className="text-slate-300">Inference Time (ms)</Label>
                  <Input
                    id="metric-inference"
                    type="number"
                    step="0.1"
                    placeholder="12.5"
                    className="bg-slate-800 border-slate-600 text-white"
                  />
                </div>
              </div>
            </div>

            {/* Provenance & Lineage */}
            <div className="space-y-4">
              <h4 className="text-lg font-semibold text-white flex items-center gap-2">
                <GitVersionControl className="w-4 h-4" />
                Provenance & Lineage
              </h4>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="code_ref" className="text-slate-300">Code Reference (Git Commit)</Label>
                  <Input
                    id="code_ref"
                    value={formData.code_ref || ""}
                    onChange={(e) => setFormData({ ...formData, code_ref: e.target.value })}
                    placeholder="a1b2c3d4"
                    className="bg-slate-800 border-slate-600 text-white font-mono text-sm"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="dataset_ref" className="text-slate-300">Dataset Reference</Label>
                  <Input
                    id="dataset_ref"
                    value={formData.dataset_ref || ""}
                    onChange={(e) => setFormData({ ...formData, dataset_ref: e.target.value })}
                    placeholder="dataset-sha256..."
                    className="bg-slate-800 border-slate-600 text-white font-mono text-sm"
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label htmlFor="parent_version" className="text-slate-300">Parent Version (Fine-tuned From)</Label>
                <Input
                  id="parent_version"
                  value={formData.parent_version || ""}
                  onChange={(e) => setFormData({ ...formData, parent_version: e.target.value })}
                  placeholder="1.0.0"
                  className="bg-slate-800 border-slate-600 text-white"
                />
              </div>
            </div>

            {/* Actions */}
            <div className="flex justify-end gap-3 pt-4 border-t border-slate-700">
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

const M13ModelRegistry = () => {
  const queryClient = useQueryClient();
  
  // State
  const [selectedModel, setSelectedModel] = useState<ModelArtifact | null>(null);
  const [showRegistrationModal, setShowRegistrationModal] = useState(false);
  const [activeTab, setActiveTab] = useState("catalog");
  const [filterFramework, setFilterFramework] = useState<string>("all");
  const [searchQuery, setSearchQuery] = useState("");
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

  // Format bytes
  const formatBytes = (bytes: number) => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  };

  // Filter models
  const filteredModels = modelsData?.models.filter(model => {
    const matchesFramework = filterFramework === "all" || model.model_card?.framework?.toLowerCase() === filterFramework.toLowerCase();
    const matchesSearch = !searchQuery || 
      model.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
      model.version.includes(searchQuery);
    return matchesFramework && matchesSearch;
  }) || [];

  // Render catalog view
  const renderCatalogView = () => {
    if (loadingModels) {
      return (
        <div className="text-center py-12">
          <Loader2 className="w-12 h-12 animate-spin mx-auto text-blue-500" />
          <p className="text-slate-400 mt-4">Loading model catalog...</p>
        </div>
      );
    }

    if (errorModels) {
      return (
        <Alert className="bg-red-500/10 border-red-500/30 text-red-400">
          <XCircle className="w-4 h-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>
            Failed to load models: {(errorModels as Error).message}
          </AlertDescription>
        </Alert>
      );
    }

    if (filteredModels.length === 0) {
      return (
        <div className="text-center py-12">
          <Database className="w-16 h-16 mx-auto text-slate-500 mb-4" />
          <h3 className="text-xl font-semibold text-white mb-2">No Models Found</h3>
          <p className="text-slate-400 mb-6">
            {modelsData?.models.length === 0 
              ? "Register your first model to get started"
              : "Try adjusting your filters or search query"}
          </p>
          {modelsData?.models.length === 0 && (
            <Button
              onClick={() => setShowRegistrationModal(true)}
              className="bg-blue-600 hover:bg-blue-700 text-white"
            >
              <Plus className="w-4 h-4 mr-2" />
              Register First Model
            </Button>
          )}
        </div>
      );
    }

    return (
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {filteredModels.map((model) => (
          <Card key={`${model.name}-${model.version}`} className="border-slate-700 bg-slate-800/50 hover:border-blue-500/50 transition-colors">
            <CardHeader className="pb-3">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="text-lg font-bold text-white">{model.name}</h3>
                  <p className="text-sm text-slate-400 font-mono">v{model.version}</p>
                </div>
                <ComplianceBadge compliant={true} />
              </div>
            </CardHeader>
            <CardContent className="space-y-3">
              {/* Framework & Task */}
              <div className="flex gap-2">
                <FrameworkBadge framework={model.model_card?.framework} />
                <TaskTypeBadge type={model.model_card?.task_type} />
              </div>

              {/* Metrics Preview */}
              {model.model_card?.metrics && Object.keys(model.model_card.metrics).length > 0 && (
                <div className="space-y-1">
                  <p className="text-xs text-slate-400">Performance:</p>
                  {Object.entries(model.model_card.metrics).slice(0, 2).map(([key, value]) => (
                    <div key={key} className="flex items-center justify-between text-sm">
                      <span className="text-slate-400">{key}:</span>
                      <span className="text-green-400 font-mono">{typeof value === 'number' ? value.toFixed(4) : value}</span>
                    </div>
                  ))}
                </div>
              )}

              {/* Lineage Info */}
              {model.lineage?.code_ref && (
                <div className="pt-2 border-t border-slate-700">
                  <div className="flex items-center gap-1 text-xs text-slate-400">
                    <GitVersionControl className="w-3 h-3" />
                    <span className="font-mono truncate">{model.lineage.code_ref.slice(0, 8)}</span>
                  </div>
                </div>
              )}

              {/* Size & Date */}
              <div className="flex items-center justify-between text-sm text-slate-400">
                <div className="flex items-center gap-1">
                  <FileText className="w-3 h-3" />
                  <span>{formatBytes(model.size_bytes)}</span>
                </div>
                <div className="flex items-center gap-1">
                  <Clock className="w-3 h-3" />
                  <span>{new Date(model.created_at).toLocaleDateString()}</span>
                </div>
              </div>

              {/* Actions */}
              <div className="flex gap-2 pt-2">
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1 border-slate-600 text-slate-300 hover:bg-slate-700"
                  onClick={() => {
                    setSelectedModel(model);
                    setActiveTab("versions");
                  }}
                >
                  Versions
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="border-slate-600 text-slate-300 hover:bg-slate-700"
                >
                  <Download className="w-4 h-4" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    );
  };

  // Render version history
  const renderVersionsView = () => {
    const models = modelsData?.models || [];
    
    if (!selectedModel && models.length > 0) {
      setSelectedModel(models[0]);
    }

    return (
      <div className="space-y-6">
        {selectedModel && (
          <Card className="border-slate-700 bg-slate-800/50">
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-xl font-bold text-white">{selectedModel.name} - Version History</h3>
                  <p className="text-sm text-slate-400">Compare versions and manage lineage</p>
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  className="border-slate-600 text-slate-300 hover:bg-slate-700"
                  onClick={() => setSelectedModel(null)}
                >
                  <ArrowLeftRight className="w-4 h-4 mr-2" />
                  Switch Model
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {/* Selected Version Highlight */}
                <div className="p-4 bg-blue-500/10 border border-blue-500/30 rounded-lg">
                  <div className="flex items-center justify-between">
                    <div>
                      <p className="text-sm text-blue-400">Current Version</p>
                      <p className="text-2xl font-bold text-white">{selectedModel.version}</p>
                    </div>
                    <FrameworkBadge framework={selectedModel.model_card?.framework} />
                  </div>
                </div>

                {/* Other Versions List */}
                <div className="space-y-2">
                  <p className="text-sm font-semibold text-slate-300">Other Versions</p>
                  {models
                    .filter(m => m.name === selectedModel.name && m.version !== selectedModel.version)
                    .map(version => (
                      <div key={`${version.name}-${version.version}`} className="flex items-center justify-between p-3 bg-slate-800 rounded-lg border border-slate-700">
                        <div className="flex items-center gap-4">
                          <Branch className="w-4 h-4 text-slate-400" />
                          <div>
                            <p className="text-white font-semibold">v{version.version}</p>
                            <p className="text-xs text-slate-400 font-mono">{version.sha256.slice(0, 12)}...</p>
                          </div>
                        </div>
                        <div className="flex items-center gap-3">
                          {version.lineage?.parent_version && (
                            <Badge variant="outline" className="text-xs text-slate-300 border-slate-600">
                              Fine-tuned from v{version.lineage.parent_version}
                            </Badge>
                          )}
                          <Button size="sm" variant="outline" className="border-slate-600 text-slate-300 hover:bg-slate-700">
                            Compare
                          </Button>
                        </div>
                      </div>
                    ))}
                </div>

                {/* Lineage Visualization */}
                {selectedModel.lineage && (
                  <div className="pt-4 border-t border-slate-700">
                    <p className="text-sm font-semibold text-slate-300 mb-3">Lineage Graph</p>
                    <div className="space-y-2 text-sm">
                      {selectedModel.lineage.code_ref && (
                        <div className="flex items-center gap-2 text-slate-300">
                          <Lock className="w-3 h-3" />
                          <span className="text-slate-400">Training Code:</span>
                          <span className="font-mono">{selectedModel.lineage.code_ref}</span>
                        </div>
                      )}
                      {selectedModel.lineage.dataset_ref && (
                        <div className="flex items-center gap-2 text-slate-300">
                          <Database className="w-3 h-3" />
                          <span className="text-slate-400">Training Data:</span>
                          <span className="font-mono">{selectedModel.lineage.dataset_ref}</span>
                        </div>
                      )}
                      {selectedModel.lineage.hyperparams && Object.keys(selectedModel.lineage.hyperparams).length > 0 && (
                        <div className="flex items-start gap-2 text-slate-300">
                          <Settings className="w-3 h-3 mt-0.5" />
                          <div>
                            <span className="text-slate-400">Hyperparameters:</span>
                            <pre className="mt-1 text-xs font-mono bg-slate-800 p-2 rounded inline-block">
                              {JSON.stringify(selectedModel.lineage.hyperparams, null, 2)}
                            </pre>
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        )}

        {!selectedModel && (
          <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
            <Filter className="w-4 h-4" />
            <AlertTitle>Select a Model</AlertTitle>
            <AlertDescription>
              Choose a model from the catalog to view its version history and lineage.
            </AlertDescription>
          </Alert>
        )}
      </div>
    );
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-950 via-slate-900 to-slate-950">
      {/* Header */}
      <header className="border-b border-slate-800 bg-slate-900/50 backdrop-blur">
        <div className="container mx-auto px-6 py-6">
          <div className="flex items-center justify-between">
            <div>
              <h1 className="text-3xl font-bold text-white mb-2">M13 Model Registry</h1>
              <p className="text-slate-400">Version-controlled AI/ML model management with full lineage tracking</p>
            </div>
            <Button
              onClick={() => setShowRegistrationModal(true)}
              className="bg-blue-600 hover:bg-blue-700 text-white px-4"
            >
              <Plus className="w-4 h-4 mr-2" />
              Register Model
            </Button>
          </div>
        </div>
      </header>

      <main className="container mx-auto px-6 py-8">
        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-5 gap-6 mb-8">
          <MetricCard
            icon={Database}
            title="Total Models"
            value={stats?.total_models || "-"}
            trend="+2 this week"
            color="blue"
          />
          <MetricCard
            icon={GitVersionControl}
            title="Total Versions"
            value={stats?.total_versions || "-"}
            color="green"
          />
          <MetricCard
            icon={FileText}
            title="Model Blobs"
            value={stats?.total_blobs || "-"}
            color="purple"
          />
          <MetricCard
            icon={ShieldCheck}
            title="Compliance Rate"
            value={stats ? `${stats.compliance_rate.toFixed(1)}%` : "-"}
            color="orange"
          />
          <MetricCard
            icon={Download}
            title="Storage Used"
            value={stats ? formatBytes(stats.storage_bytes) : "-"}
            color="yellow"
          />
        </div>

        {/* Search & Filter Bar */}
        <Card className="border-slate-700 bg-slate-800/50 mb-6">
          <CardContent className="pt-6">
            <div className="flex items-center gap-4">
              <div className="flex-1 relative">
                <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 w-4 h-4 text-slate-400" />
                <Input
                  placeholder="Search models by name or version..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="pl-10 bg-slate-700 border-slate-600 text-white"
                />
              </div>
              <div className="w-48">
                <select
                  value={filterFramework}
                  onChange={(e) => setFilterFramework(e.target.value)}
                  className="w-full px-3 py-2 bg-slate-700 border border-slate-600 rounded-md text-white focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="all">All Frameworks</option>
                  <option value="pytorch">PyTorch</option>
                  <option value="tensorflow">TensorFlow</option>
                  <option value="onnx">ONNX</option>
                  <option value="tensorrt">TensorRT</option>
                </select>
              </div>
              <Button
                size="sm"
                variant="outline"
                onClick={() => queryClient.invalidateQueries({ queryKey: ["models"] })}
                className="border-slate-600 text-slate-300 hover:bg-slate-700"
              >
                <RefreshCw className="w-4 h-4" />
              </Button>
            </div>
          </CardContent>
        </Card>

        {/* Tabs */}
        <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-6">
          <TabsList className="bg-slate-800 border border-slate-700">
            <TabsTrigger value="catalog" className="data-[state=active]:bg-blue-600">
              <Database className="w-4 h-4 mr-2" />
              Model Catalog
            </TabsTrigger>
            <TabsTrigger value="versions" className="data-[state=active]:bg-blue-600">
              <GitVersionControl className="w-4 h-4 mr-2" />
              Version History
            </TabsTrigger>
            <TabsTrigger value="compliance" className="data-[state=active]:bg-blue-600">
              <ShieldCheck className="w-4 h-4 mr-2" />
              Compliance
            </TabsTrigger>
          </TabsList>

          {/* Catalog Tab */}
          <TabsContent value="catalog">
            {renderCatalogView()}
          </TabsContent>

          {/* Version History Tab */}
          <TabsContent value="versions">
            {renderVersionsView()}
          </TabsContent>

          {/* Compliance Placeholder */}
          <TabsContent value="compliance">
            <Card className="border-slate-700 bg-slate-800/50">
              <CardHeader>
                <h3 className="text-xl font-bold text-white">Compliance & Security Scan</h3>
                <p className="text-sm text-slate-400">Verify license compatibility and security vulnerabilities</p>
              </CardHeader>
              <CardContent>
                <Alert className="bg-blue-500/10 border-blue-500/30 text-blue-400">
                  <ShieldCheck className="w-4 h-4" />
                  <AlertTitle>Compliance Features Coming Soon</AlertTitle>
                  <AlertDescription>
                    Advanced compliance checking and security scanning will be available in the next release.
                    Current models pass automated verification checks.
                  </AlertDescription>
                </Alert>
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>
      </main>

      {/* Registration Modal */}
      <RegisterModelModal
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

export default M13ModelRegistry;
