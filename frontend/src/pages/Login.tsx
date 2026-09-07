import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@radix-ui/react-label";
import { Loader2, Shield, Lock, AlertTriangle } from "lucide-react";
import { useAuthStore } from "@/stores/auth-store";

interface LoginForm {
  username: string;
  password: string;
}

export function LoginPage() {
  const [formData, setFormData] = useState<LoginForm>({
    username: "",
    password: "",
  });
  
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  
  const navigate = useNavigate();
  const login = useAuthStore((state) => state.login);
  const clearError = useAuthStore((state) => state.clearError);
  
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    if (!formData.username || !formData.password) {
      setError("Please fill in all fields");
      return;
    }
    
    setIsLoading(true);
    setError(null);
    clearError();
    
    try {
      await login(formData.username, formData.password);
      
      // Redirect based on user role
      setTimeout(() => {
        navigate("/dashboard");
      }, 500);
    } catch (err: any) {
      setError(err.message || "Invalid credentials or unauthorized access");
    } finally {
      setIsLoading(false);
    }
  };
  
  const handleDemoLogin = async () => {
    setFormData({
      username: "demo",
      password: "password",
    });
    
    handleSubmit(new Event("submit") as any);
  };
  
  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-gray-900 via-slate-900 to-gray-900 relative overflow-hidden">
      {/* Animated background elements */}
      <div className="absolute inset-0 overflow-hidden">
        <div className="absolute -top-[50%] -left-[25%] w-[100%] h-[100%] bg-gradient-to-r from-red-600/20 via-transparent to-orange-600/20 animate-spin" 
             style={{ animationDuration: "20s" }} />
        <div className="absolute top-[25%] -right-[25%] w-[100%] h-[100%] bg-gradient-to-l from-blue-600/10 via-transparent to-purple-600/10 animate-pulse"
             style={{ animationDuration: "15s" }} />
      </div>
      
      {/* Grid pattern overlay */}
      <div className="absolute inset-0 bg-grid-white/[0.02]" 
           style={{
             backgroundImage: `linear-gradient(rgba(255, 255, 255, 0.03) 1px, transparent 1px), linear-gradient(90deg, rgba(255, 255, 255, 0.03) 1px, transparent 1px)`,
             backgroundSize: '64px 64px'
           }} 
      />
      
      {/* Main login card */}
      <Card className="w-full max-w-md shadow-2xl border-t-4 border-red-600 z-10 glass-effect backdrop-blur-xl">
        <CardHeader className="text-center space-y-6 pb-8">
          {/* Logo with shield icon */}
          <div className="mx-auto w-20 h-20 rounded-full bg-gradient-to-br from-red-600 to-orange-600 flex items-center justify-center shadow-lg shadow-red-600/30 animate-in slide-in-from-bottom-4 duration-700">
            <Shield className="w-10 h-10 text-white" strokeWidth={1.5} />
          </div>
          
          <CardTitle className="text-3xl font-bold tracking-tight">
            <span className="gradient-text">CloudAI Fusion</span>
            <br />
            Red Team Platform
          </CardTitle>
          
          <CardDescription className="text-gray-400 text-base leading-relaxed pt-2">
            Enterprise-grade Penetration Testing & Vulnerability Assessment Platform
          </CardDescription>
          
          {/* Feature badges */}
          <div className="flex gap-2 justify-center pt-4">
            <div className="flex items-center gap-1 px-3 py-1.5 rounded-full bg-red-500/10 border border-red-500/20">
              <Lock className="w-3.5 h-3.5 text-red-500" />
              <span className="text-xs text-gray-400">Verified AI</span>
            </div>
            <div className="flex items-center gap-1 px-3 py-1.5 rounded-full bg-blue-500/10 border border-blue-500/20">
              <Shield className="w-3.5 h-3.5 text-blue-500" />
              <span className="text-xs text-gray-400">Compliant</span>
            </div>
          </div>
        </CardHeader>
        
        <CardContent>
          {/* Error alert */}
          {error && (
            <div className="mb-6 p-4 rounded-lg bg-red-500/10 border border-red-500/20 flex items-start gap-3 animate-in fade-in duration-300">
              <AlertTriangle className="w-5 h-5 text-red-500 mt-0.5 shrink-0" />
              <p className="text-sm text-red-400">{error}</p>
            </div>
          )}
          
          {/* Login form */}
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="username" className="text-sm font-medium text-gray-300">
                Username
              </Label>
              <Input
                id="username"
                type="text"
                placeholder="Enter your username"
                value={formData.username}
                onChange={(e) => setFormData({...formData, username: e.target.value})}
                disabled={isLoading}
                className="h-12 bg-slate-800/50 border-slate-700 text-white placeholder:text-gray-500 hover:border-slate-600 focus:border-red-500 transition-colors"
                autoComplete="username"
              />
            </div>
            
            <div className="space-y-2">
              <Label htmlFor="password" className="text-sm font-medium text-gray-300">
                Password
              </Label>
              <Input
                id="password"
                type="password"
                placeholder="••••••••"
                value={formData.password}
                onChange={(e) => setFormData({...formData, password: e.target.value})}
                disabled={isLoading}
                className="h-12 bg-slate-800/50 border-slate-700 text-white placeholder:text-gray-500 hover:border-slate-600 focus:border-red-500 transition-colors"
                autoComplete="current-password"
              />
            </div>
            
            <Button 
              type="submit" 
              disabled={isLoading}
              className="w-full h-12 bg-gradient-to-r from-red-600 to-orange-600 hover:from-red-700 hover:to-orange-700 text-white font-semibold text-base shadow-lg shadow-red-600/25 transition-all duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isLoading ? (
                <>
                  <Loader2 className="w-5 h-5 mr-2 animate-spin" />
                  Authenticating...
                </>
              ) : (
                <>
                  <Lock className="w-5 h-5 mr-2" />
                  Sign In
                </>
              )}
            </Button>
          </form>
          
          {/* Demo mode note */}
          <div className="mt-6 p-4 rounded-lg bg-blue-500/5 border border-blue-500/15">
            <div className="flex items-start gap-3">
              <div className="w-1.5 h-1.5 rounded-full bg-blue-500 mt-2" />
              <div className="flex-1">
                <p className="text-sm text-gray-400">
                  <span className="text-blue-400 font-medium">Sandbox Mode:</span> No license required for testing • Production mode requires approved work order
                </p>
              </div>
            </div>
          </div>
          
          {/* Quick demo login */}
          <div className="mt-6">
            <Button
              type="button"
              variant="ghost"
              onClick={handleDemoLogin}
              disabled={isLoading}
              className="w-full text-gray-400 hover:text-white hover:bg-slate-800/50"
            >
              Try Demo Account →
            </Button>
          </div>
        </CardContent>
        
        <CardFooter className="px-6 pb-6">
          <div className="w-full border-t border-slate-700/50 pt-4 text-center">
            <p className="text-xs text-gray-500">
              © 2026 CloudAI Fusion. All rights reserved. ·{" "}
              <a href="/privacy" className="hover:text-gray-400 transition-colors">Privacy Policy</a> ·{" "}
              <a href="/terms" className="hover:text-gray-400 transition-colors">Terms of Service</a>
            </p>
          </div>
        </CardFooter>
      </Card>
    </div>
  );
}
