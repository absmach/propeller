"use client"

import { useState } from "react"
import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { ThemeToggle } from "../../../components/theme-toggle"
import { 
  Rocket, 
  Mail, 
  ArrowLeft,
  CheckCircle,
  ArrowRight,
  Shield
} from "lucide-react"

export default function ForgotPasswordPage() {
  const [isLoading, setIsLoading] = useState(false)
  const [isSuccess, setIsSuccess] = useState(false)
  const [email, setEmail] = useState("")

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setIsLoading(true)
    
    // Simulate API call
    await new Promise(resolve => setTimeout(resolve, 2000))
    
    console.log("Password reset requested for:", email)
    setIsSuccess(true)
    setIsLoading(false)
  }

  const handleResend = async () => {
    setIsLoading(true)
    await new Promise(resolve => setTimeout(resolve, 1000))
    console.log("Resending password reset email to:", email)
    setIsLoading(false)
  }

  if (isSuccess) {
    return (
      <div className="min-h-screen bg-background">
        {/* Header */}
        <header className="border-b">
          <div className="container mx-auto px-4 py-4 flex items-center justify-between">
            <Link href="/" className="flex items-center space-x-2 hover:opacity-80 transition-opacity">
              <ArrowLeft className="h-4 w-4" />
              <Rocket className="h-8 w-8 text-primary" />
              <h1 className="text-2xl font-bold">Propeller</h1>
            </Link>
            <ThemeToggle />
          </div>
        </header>

        {/* Success Content */}
        <div className="container mx-auto px-4 py-20">
          <div className="max-w-md mx-auto">
            <div className="text-center mb-8">
              <Badge variant="secondary" className="mb-4">
                <Shield className="h-3 w-3 mr-1" />
                Check Your Email
              </Badge>
              <h1 className="text-3xl font-bold tracking-tight mb-2">
                Reset link sent!
              </h1>
              <p className="text-muted-foreground">
                We've sent a password reset link to your email address
              </p>
            </div>

            <Card>
              <CardHeader className="space-y-1">
                <div className="flex justify-center mb-4">
                  <div className="w-16 h-16 bg-green-100 dark:bg-green-900/20 rounded-full flex items-center justify-center">
                    <CheckCircle className="h-8 w-8 text-green-600 dark:text-green-400" />
                  </div>
                </div>
                <CardTitle className="text-2xl text-center">Email Sent</CardTitle>
                <CardDescription className="text-center">
                  Check your inbox for the password reset link
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="bg-muted/50 rounded-lg p-4">
                  <p className="text-sm text-muted-foreground text-center">
                    We've sent a password reset link to:
                  </p>
                  <p className="text-sm font-medium text-center mt-1">
                    {email}
                  </p>
                </div>

                <div className="space-y-3">
                  <Button 
                    onClick={handleResend}
                    variant="outline" 
                    className="w-full" 
                    disabled={isLoading}
                  >
                    {isLoading ? "Sending..." : "Resend email"}
                  </Button>
                  
                  <Link href="/login">
                    <Button variant="ghost" className="w-full">
                      Back to login
                    </Button>
                  </Link>
                </div>

                <div className="text-center text-sm text-muted-foreground">
                  <p>Didn't receive the email? Check your spam folder or</p>
                  <Button 
                    variant="link" 
                    className="p-0 h-auto text-sm"
                    onClick={handleResend}
                    disabled={isLoading}
                  >
                    try a different email address
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-b">
        <div className="container mx-auto px-4 py-4 flex items-center justify-between">
          <Link href="/login" className="flex items-center space-x-2 hover:opacity-80 transition-opacity">
            <ArrowLeft className="h-4 w-4" />
            <Rocket className="h-8 w-8 text-primary" />
            <h1 className="text-2xl font-bold">Propeller</h1>
          </Link>
          <ThemeToggle />
        </div>
      </header>

      {/* Main Content */}
      <div className="container mx-auto px-4 py-20">
        <div className="max-w-md mx-auto">
          <div className="text-center mb-8">
            <Badge variant="secondary" className="mb-4">
              <Shield className="h-3 w-3 mr-1" />
              Password Recovery
            </Badge>
            <h1 className="text-3xl font-bold tracking-tight mb-2">
              Forgot your password?
            </h1>
            <p className="text-muted-foreground">
              No worries! Enter your email and we'll send you reset instructions
            </p>
          </div>

          <Card>
            <CardHeader className="space-y-1">
              <CardTitle className="text-2xl text-center">Reset Password</CardTitle>
              <CardDescription className="text-center">
                Enter your email address to receive a password reset link
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <form onSubmit={handleSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label htmlFor="email">Email</Label>
                  <div className="relative">
                    <Mail className="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
                    <Input
                      id="email"
                      name="email"
                      type="email"
                      placeholder="Enter your email"
                      value={email}
                      onChange={(e) => setEmail(e.target.value)}
                      className="pl-10"
                      required
                    />
                  </div>
                </div>

                <Button 
                  type="submit" 
                  className="w-full" 
                  disabled={isLoading}
                >
                  {isLoading ? (
                    <>
                      <div className="animate-spin rounded-full h-4 w-4 border-b-2 border-white mr-2"></div>
                      Sending reset link...
                    </>
                  ) : (
                    <>
                      Send reset link
                      <ArrowRight className="h-4 w-4 ml-2" />
                    </>
                  )}
                </Button>
              </form>

              <div className="text-center text-sm">
                Remember your password?{" "}
                <Link
                  href="/login"
                  className="text-primary hover:underline font-medium"
                >
                  Sign in
                </Link>
              </div>

              <div className="bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
                <div className="flex items-start space-x-3">
                  <Shield className="h-5 w-5 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
                  <div className="text-sm">
                    <p className="font-medium text-blue-900 dark:text-blue-100 mb-1">
                      Security Notice
                    </p>
                    <p className="text-blue-700 dark:text-blue-300">
                      The reset link will expire in 1 hour for your security. 
                      If you didn't request this, you can safely ignore this email.
                    </p>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
} 