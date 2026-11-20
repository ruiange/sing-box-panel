import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const backend = process.env.PANEL_DEV_API ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    host: '127.0.0.1',
    proxy: {
      '/api': { target: backend, ws: true },
      '/sub': backend,
      '/docs': backend,
      '/openapi': backend,
    },
  },
})
