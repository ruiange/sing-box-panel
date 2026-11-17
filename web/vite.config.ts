import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const backend = process.env.PANEL_DEV_API ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [vue()],
  server: {
    host: '127.0.0.1',
    proxy: {
      '/api': { target: backend, ws: true },
      '/docs': backend,
      '/openapi': backend,
    },
  },
})
