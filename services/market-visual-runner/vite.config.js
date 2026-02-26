import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  base: '/market-visual-runner/',
  plugins: [vue()],
  server: {
    allowedHosts: ['ivanna-unwhispering-histologically.ngrok-free.dev'],
  },
})
