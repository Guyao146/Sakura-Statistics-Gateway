import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: { proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: false } } },
  build: { rollupOptions: { output: { manualChunks(id) {
    if (id.includes('/node_modules/zrender/')) return 'chart-renderer'
    if (id.includes('/node_modules/echarts/')) return 'charts'
    if (id.includes('/node_modules/@vue/') || id.includes('/node_modules/vue/')) return 'vue'
  } } } },
})
