import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { ArrowRight, ArrowLeft, CheckCircle2, FileText, Upload, Shield, Target } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { apiClient } from "@/lib/api";
import { useAuthStore } from "@/stores/auth-store";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";

interface WorkOrderForm {
  companyName: string;
  email: string;
  justification: string;
  targets: string;
}

export function WorkOrderSubmitPage() {
  const [step, setStep] = useState(1);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitSuccess, setSubmitSuccess] = useState(false);
  
  const [formData, setFormData] = useState<WorkOrderForm>({
    companyName: "",
    email: "",
    justification: "",
    targets: "",
  });
  
  const navigate = useNavigate();
  const logout = useAuthStore((state) => state.logout);
  
  const steps = [
    { id: 1, title: "Company Details" },
    { id: 2, title: "Target Systems" },
    { id: 3, title: "Authorization" },
    { id: 4, title: "Confirmation" },
  ];
  
  const currentStepConfig = [
    {
      title: "Company Information",
      subtitle: "Provide your organization details for work order processing",
      icon: <Shield className="w-6 h-6" />,
    },
    {
      title: "Target Systems",
      subtitle: "Specify the systems you want to assess (only those authorized for your tenant)",
      icon: <Target className="w-6 h-6" />,
    },
    {
      title: "Upload Authorization",
      subtitle: "Attach signed authorization letter and compliance documentation",
      icon: <FileText className="w-6 h-6" />,
    },
    {
      title: "Review & Submit",
      subtitle: "Verify information before submission",
      icon: <CheckCircle2 className="w-6 h-6" />,
    },
  ];
  
  const handleNext = () => step < 4 && setStep(step + 1);
  const handleBack = () => step > 1 && setStep(step - 1);
  
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    if (step !== 4) {
      handleNext();
      return;
    }
    
    setIsSubmitting(true);
    
    try {
      await apiClient.submitWorkOrder(formData);
      setSubmitSuccess(true);
      
      setTimeout(() => {
        navigate("/dashboard");
      }, 3000);
    } catch (error: any) {
      alert(error.message || "Failed to submit work order");
      setIsSubmitting(false);
    }
  };
  
  if (submitSuccess) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 flex items-center justify-center">
        <Card className="w-full max-w-md glass-effect backdrop-blur-xl border-green-500/30">
          <CardContent className="pt-8">
            <div className="text-center space-y-6 animate-in zoom-in duration-500">
              <div className="mx-auto w-16 h-16 rounded-full bg-green-500/10 flex items-center justify-center">
                <CheckCircle2 className="w-8 h-8 text-green-500" />
              </div>
              
              <h2 className="text-2xl font-bold text-white">Application Submitted!</h2>
              <p className="text-gray-400">
                Your work order request has been submitted for approval. We'll review it within 24-48 hours.
              </p>
              
              <div className="space-y-3 pt-4">
                <Button onClick={() => navigate("/dashboard")} className="w-full">
                  Return to Dashboard
                </Button>
                <Button variant="ghost" onClick={logout} className="w-full text-gray-400">
                  Logout
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }
  
  return (
    <div className="min-h-screen bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 py-12 px-4">
      <div className="max-w-3xl mx-auto">
        {/* Header */}
        <div className="text-center mb-12 animate-in fade-in slide-in-from-bottom-4 duration-700">
          <div className="flex items-center justify-center gap-3 mb-4">
            <Shield className="w-10 h-10 text-red-600" />
            <h1 className="text-3xl font-bold gradient-text">Production Access Request</h1>
          </div>
          <p className="text-gray-400">Submit a work order to enable production penetration testing capabilities</p>
        </div>
        
        {/* Progress Steps */}
        <div className="mb-12 animate-in fade-in slide-in-from-bottom-8 duration-1000 delay-100">
          <Progress value={(step / 4) * 100} className="h-1 mb-4 bg-slate-700" />
          <div className="grid grid-cols-4 gap-4">
            {steps.map((s, i) => (
              <div key={s.id} className="flex items-center gap-3">
                <div
                  className={`w-8 h-8 rounded-full flex items-center justify-center font-semibold text-sm transition-all duration-300 ${
                    step >= s.id
                      ? "bg-gradient-to-r from-red-600 to-orange-600 text-white shadow-lg shadow-red-600/30"
                      : "bg-slate-700 text-gray-400"
                  }`}
                >
                  {step > s.id ? <CheckCircle2 className="w-4 h-4" /> : s.id}
                </div>
                <span className={`text-xs hidden sm:inline ${step >= s.id ? "text-white" : "text-gray-500"}`}>
                  {s.title}
                </span>
              </div>
            ))}
          </div>
        </div>
        
        {/* Form Card */}
        <form onSubmit={handleSubmit} className="animate-in fade-in slide-in-from-bottom-8 duration-1000">
          <Card className="glass-effect backdrop-blur-lg border-slate-700/50">
            <CardHeader>
              <div className="flex items-center gap-3">
                <div className="p-2 rounded-lg bg-red-500/10">
                  {currentStepConfig[step - 1].icon}
                </div>
                <div>
                  <CardTitle className="text-xl text-white">{currentStepConfig[step - 1].title}</CardTitle>
                  <p className="text-sm text-gray-400">{currentStepConfig[step - 1].subtitle}</p>
                </div>
              </div>
            </CardHeader>
            
            <CardContent className="space-y-6">
              {/* Step 1: Company Details */}
              {step === 1 && (
                <div className="space-y-4 animate-in fade-in slide-in-from-left-4 duration-300">
                  <div>
                    <Label htmlFor="companyName" className="text-gray-300 mb-2 block">Company Name *</Label>
                    <Input
                      id="companyName"
                      placeholder="Acme Corporation"
                      value={formData.companyName}
                      onChange={(e) => setFormData({...formData, companyName: e.target.value})}
                      required
                      className="bg-slate-800/50 border-slate-700 text-white placeholder:text-gray-500"
                    />
                  </div>
                  
                  <div>
                    <Label htmlFor="email" className="text-gray-300 mb-2 block">Email Address *</Label>
                    <Input
                      id="email"
                      type="email"
                      placeholder="contact@acme.com"
                      value={formData.email}
                      onChange={(e) => setFormData({...formData, email: e.target.value})}
                      required
                      className="bg-slate-800/50 border-slate-700 text-white placeholder:text-gray-500"
                    />
                  </div>
                  
                  <div>
                    <Label htmlFor="justification" className="text-gray-300 mb-2 block">Business Justification *</Label>
                    <textarea
                      id="justification"
                      rows={5}
                      placeholder="Explain why you need production access to our Red Team platform..."
                      value={formData.justification}
                      onChange={(e) => setFormData({...formData, justification: e.target.value})}
                      required
                      className="w-full min-h-[150px] p-4 bg-slate-800/50 border border-slate-700 rounded-lg text-white placeholder:text-gray-500 focus:outline-none focus:ring-2 focus:ring-red-500 resize-none"
                    />
                  </div>
                  
                  <div className="flex justify-between pt-4">
                    <Button
                      type="button"
                      onClick={handleBack}
                      variant="ghost"
                      disabled={step === 1}
                      className="text-gray-400 hover:text-white"
                    >
                      <ArrowLeft className="w-4 h-4 mr-2" />
                      Back
                    </Button>
                    <Button type="button" onClick={handleNext} className="bg-red-600 hover:bg-red-700 text-white">
                      Next <ArrowRight className="w-4 h-4 ml-2" />
                    </Button>
                  </div>
                </div>
              )}
              
              {/* Step 2: Target Systems */}
              {step === 2 && (
                <div className="space-y-4 animate-in fade-in slide-in-from-right-4 duration-300">
                  <div>
                    <Label htmlFor="targets" className="text-gray-300 mb-2 block">Target IP Addresses / CIDR Ranges *</Label>
                    <textarea
                      id="targets"
                      rows={6}
                      placeholder="Enter target systems you want to assess:&#13;&#10;Example:&#13;&#10;192.168.1.0/24&#13;&#10;10.0.0.1&#13;&#10;https://internal-app.company.com"
                      value={formData.targets}
                      onChange={(e) => setFormData({...formData, targets: e.target.value})}
                      required
                      className="w-full min-h-[200px] p-4 bg-slate-800/50 border border-slate-700 rounded-lg text-white placeholder:text-gray-500 focus:outline-none focus:ring-2 focus:ring-red-500 font-mono text-sm resize-none"
                    />
                    <p className="mt-2 text-xs text-gray-500">
                      ⚠️ Only targets authorized for your tenant are allowed. Unauthorized scanning is prohibited.
                    </p>
                  </div>
                  
                  <div className="flex justify-between pt-4">
                    <Button
                      type="button"
                      onClick={handleBack}
                      variant="outline"
                      className="border-slate-700 hover:bg-slate-800 text-gray-300"
                    >
                      <ArrowLeft className="w-4 h-4 mr-2" />
                      Back
                    </Button>
                    <Button type="button" onClick={handleNext} className="bg-red-600 hover:bg-red-700 text-white">
                      Next <ArrowRight className="w-4 h-4 ml-2" />
                    </Button>
                  </div>
                </div>
              )}
              
              {/* Step 3: Upload Authorization */}
              {step === 3 && (
                <div className="space-y-4 animate-in fade-in slide-in-from-right-4 duration-300">
                  <div>
                    <Label className="text-gray-300 mb-2 block">Signed Authorization Letter (PDF) *</Label>
                    <div className="border-2 border-dashed border-slate-700 rounded-lg p-8 text-center hover:border-red-500/50 transition-colors cursor-pointer group">
                      <Upload className="w-12 h-12 text-gray-500 group-hover:text-red-500 mx-auto mb-4" />
                      <p className="text-sm text-gray-400">
                        Drop your signed authorization letter here or click to browse
                      </p>
                      <p className="text-xs text-gray-500 mt-2">PDF only, maximum 10MB</p>
                    </div>
                  </div>
                  
                  <div>
                    <Label htmlFor="additionalDocs" className="text-gray-300 mb-2 block">Additional Documentation</Label>
                    <input
                      id="additionalDocs"
                      type="file"
                      accept=".pdf,.doc,.docx"
                      className="block w-full text-sm text-gray-400 file:mr-4 file:py-2 file:px-4 file:rounded-md file:border-0 file:text-sm file:font-semibold file:bg-slate-700 file:text-gray-300 hover:file:bg-slate-600"
                    />
                  </div>
                  
                  <div className="bg-blue-500/10 border border-blue-500/20 rounded-lg p-4">
                    <p className="text-sm text-blue-400">
                      ℹ️ Ensure your authorization letter includes: signature from authorized representative, scope definition, timeline, and contact information.
                    </p>
                  </div>
                  
                  <div className="flex justify-between pt-4">
                    <Button
                      type="button"
                      onClick={handleBack}
                      variant="outline"
                      className="border-slate-700 hover:bg-slate-800 text-gray-300"
                    >
                      <ArrowLeft className="w-4 h-4 mr-2" />
                      Back
                    </Button>
                    <Button type="button" onClick={handleNext} className="bg-red-600 hover:bg-red-700 text-white">
                      Review & Submit <CheckCircle2 className="w-4 h-4 ml-2" />
                    </Button>
                  </div>
                </div>
              )}
              
              {/* Step 4: Confirmation */}
              {step === 4 && (
                <div className="space-y-6 animate-in fade-in slide-in-from-right-4 duration-300">
                  <div className="bg-slate-800/30 border border-slate-700/30 rounded-lg p-6 space-y-4">
                    <h3 className="text-lg font-semibold text-white">Summary</h3>
                    
                    <div className="grid md:grid-cols-2 gap-4">
                      <div>
                        <p className="text-sm text-gray-500">Company</p>
                        <p className="text-white font-medium">{formData.companyName}</p>
                      </div>
                      <div>
                        <p className="text-sm text-gray-500">Email</p>
                        <p className="text-white font-medium">{formData.email}</p>
                      </div>
                    </div>
                    
                    <div>
                      <p className="text-sm text-gray-500">Justification</p>
                      <p className="text-white">{formData.justification}</p>
                    </div>
                    
                    <div>
                      <p className="text-sm text-gray-500">Targets</p>
                      <p className="text-white font-mono text-sm whitespace-pre-wrap">{formData.targets}</p>
                    </div>
                  </div>
                  
                  <div className="bg-yellow-500/10 border border-yellow-500/20 rounded-lg p-4">
                    <div className="flex items-start gap-3">
                      <Shield className="w-5 h-5 text-yellow-500 mt-0.5 shrink-0" />
                      <div>
                        <p className="text-sm text-yellow-400 font-medium">Important Notice</p>
                        <p className="text-xs text-yellow-400/70 mt-1">
                          By submitting this form, you agree to comply with all applicable laws and regulations. Any unauthorized access attempts will be reported to authorities. This platform is for authorized security testing only.
                        </p>
                      </div>
                    </div>
                  </div>
                  
                  <div className="flex justify-between pt-4">
                    <Button
                      type="button"
                      onClick={handleBack}
                      variant="outline"
                      className="border-slate-700 hover:bg-slate-800 text-gray-300"
                    >
                      <ArrowLeft className="w-4 h-4 mr-2" />
                      Back
                    </Button>
                    <Button
                      type="submit"
                      disabled={isSubmitting}
                      className="bg-green-600 hover:bg-green-700 text-white disabled:opacity-50"
                    >
                      {isSubmitting ? "Submitting..." : "Confirm & Submit ✓"}
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </form>
      </div>
    </div>
  );
}
