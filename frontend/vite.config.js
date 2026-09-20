import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const securityHeaders = {
  'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
  'X-Content-Type-Options': 'nosniff',
  'X-Frame-Options': 'DENY',
  'Referrer-Policy': 'strict-origin-when-cross-origin',
  'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
}

function securityHeadersPlugin() {
  const applyHeaders = (headers) => (_request, response, next) => {
    Object.entries(headers).forEach(([name, value]) => response.setHeader(name, value))
    next()
  }

  const devHeaders = {
    ...securityHeaders,
    'Content-Security-Policy': securityHeaders['Content-Security-Policy']
      .replace("script-src 'self'", "script-src 'self' 'unsafe-inline'")
      .replace("connect-src 'self'", "connect-src 'self' ws:"),
  }

  return {
    name: 'probewatch-security-headers',
    configureServer(server) {
      server.middlewares.use(applyHeaders(devHeaders))
    },
    configurePreviewServer(server) {
      server.middlewares.use(applyHeaders(securityHeaders))
    },
  }
}

export default defineConfig({
  plugins: [react(), securityHeadersPlugin()],
})
