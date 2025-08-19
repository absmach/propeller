import Link from "next/link"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { ThemeToggle } from "../components/theme-toggle"
import { 
  Rocket, 
  Zap, 
  Shield, 
  Palette, 
  Code, 
  Globe,
  ArrowRight,
  Star
} from "lucide-react"

export default function Home() {
  return (
    <div className="min-h-screen bg-background">
      {/* Header */}
      <header className="border-b">
        <div className="container mx-auto px-4 py-4 flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Rocket className="h-8 w-8 text-primary" />
            <h1 className="text-2xl font-bold">Propeller</h1>
          </div>
          <ThemeToggle />
        </div>
      </header>

      {/* Hero Section */}
      <section className="container mx-auto px-4 py-20">
        <div className="text-center space-y-6">
          <Badge variant="secondary" className="mb-4">
            <Zap className="h-3 w-3 mr-1" />
            Powered by Next.js & Turbo
          </Badge>
          <h1 className="text-5xl font-bold tracking-tight">
            Modern UI Components
            <br />
            <span className="text-primary">Built for Speed</span>
          </h1>
          <p className="text-xl text-muted-foreground max-w-2xl mx-auto">
            A beautiful, accessible, and customizable component library built with 
            Shadcn UI, Next.js, and Turbo for lightning-fast development.
          </p>
          <div className="flex items-center justify-center space-x-4">
            <Link href="/signup">
              <Button size="lg" className="gap-2">
                Get Started
                <ArrowRight className="h-4 w-4" />
              </Button>
            </Link>
            <Link href="/login">
              <Button variant="outline" size="lg">
                Sign In
              </Button>
            </Link>
          </div>
        </div>
      </section>

      {/* Features Grid */}
      <section className="container mx-auto px-4 py-20">
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          <Card className="group hover:shadow-lg transition-all duration-300">
            <CardHeader>
              <div className="w-12 h-12 bg-primary/10 rounded-lg flex items-center justify-center mb-4">
                <Palette className="h-6 w-6 text-primary" />
              </div>
              <CardTitle>Beautiful Design</CardTitle>
              <CardDescription>
                Carefully crafted components with attention to detail and modern design principles.
              </CardDescription>
            </CardHeader>
          </Card>

          <Card className="group hover:shadow-lg transition-all duration-300">
            <CardHeader>
              <div className="w-12 h-12 bg-primary/10 rounded-lg flex items-center justify-center mb-4">
                <Code className="h-6 w-6 text-primary" />
              </div>
              <CardTitle>Developer Experience</CardTitle>
              <CardDescription>
                Built with TypeScript, fully typed components, and excellent developer tooling.
              </CardDescription>
            </CardHeader>
          </Card>

          <Card className="group hover:shadow-lg transition-all duration-300">
            <CardHeader>
              <div className="w-12 h-12 bg-primary/10 rounded-lg flex items-center justify-center mb-4">
                <Shield className="h-6 w-6 text-primary" />
              </div>
              <CardTitle>Accessible</CardTitle>
              <CardDescription>
                WCAG compliant components that work for everyone, including screen readers.
              </CardDescription>
            </CardHeader>
          </Card>

          <Card className="group hover:shadow-lg transition-all duration-300">
            <CardHeader>
              <div className="w-12 h-12 bg-primary/10 rounded-lg flex items-center justify-center mb-4">
                <Zap className="h-6 w-6 text-primary" />
              </div>
              <CardTitle>Lightning Fast</CardTitle>
              <CardDescription>
                Optimized for performance with Turbo and Next.js for the best user experience.
              </CardDescription>
            </CardHeader>
          </Card>

          <Card className="group hover:shadow-lg transition-all duration-300">
            <CardHeader>
              <div className="w-12 h-12 bg-primary/10 rounded-lg flex items-center justify-center mb-4">
                <Globe className="h-6 w-6 text-primary" />
              </div>
              <CardTitle>Responsive</CardTitle>
              <CardDescription>
                Mobile-first design that looks great on all devices and screen sizes.
              </CardDescription>
            </CardHeader>
          </Card>

          <Card className="group hover:shadow-lg transition-all duration-300">
            <CardHeader>
              <div className="w-12 h-12 bg-primary/10 rounded-lg flex items-center justify-center mb-4">
                <Star className="h-6 w-6 text-primary" />
              </div>
              <CardTitle>Customizable</CardTitle>
              <CardDescription>
                Easy to customize with CSS variables and Tailwind CSS for your brand.
              </CardDescription>
            </CardHeader>
          </Card>
        </div>
      </section>

      {/* CTA Section */}
      <section className="container mx-auto px-4 py-20">
        <Card className="bg-gradient-to-r from-primary/10 to-primary/5 border-primary/20">
          <CardContent className="pt-6">
            <div className="text-center space-y-4">
              <h2 className="text-3xl font-bold">Ready to get started?</h2>
              <p className="text-muted-foreground">
                Join thousands of developers building amazing UIs with Propeller.
              </p>
              <Button size="lg" className="gap-2">
                <Rocket className="h-4 w-4" />
                Launch Your Project
              </Button>
            </div>
          </CardContent>
        </Card>
      </section>

      {/* Footer */}
      <footer className="border-t py-8">
        <div className="container mx-auto px-4 text-center text-muted-foreground">
          <p>© 2025 Propeller UI.</p>
        </div>
      </footer>
    </div>
  )
} 