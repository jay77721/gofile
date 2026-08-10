import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  base: '/static/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // dev 模式下代理后端 API,与生产同源一致
      '/user': 'http://localhost:8080',
      '/file': 'http://localhost:8080',
      '/share': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
})
