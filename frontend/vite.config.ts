import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
  },
  css: {
    transformer: 'postcss',
  },
  build: {
    cssMinify: 'esbuild',
    outDir: 'dist',
    assetsDir: 'assets',
    rollupOptions: {
      output: {
        // 阶段2 F-2：把重型依赖拆成独立 chunk，首屏只加载核心代码，
        // g6/monaco/markdown+katex 等大体积依赖按需（懒加载）或并行缓存加载。
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined
          if (id.includes('@antv/g6') || id.includes('@antv/util') || id.includes('@antv/x6')) {
            return 'g6'
          }
          if (id.includes('monaco-editor') || id.includes('@monaco-editor')) {
            return 'monaco'
          }
          if (
            id.includes('react-markdown') ||
            id.includes('remark-') ||
            id.includes('rehype-') ||
            id.includes('katex') ||
            id.includes('highlight.js') ||
            id.includes('micromark') ||
            id.includes('mdast') ||
            id.includes('hast') ||
            id.includes('unist') ||
            id.includes('property-information') ||
            id.includes('space-separated-tokens') ||
            id.includes('comma-separated-tokens')
          ) {
            return 'markdown'
          }
          if (id.includes('@tanstack') || id.includes('react-query')) {
            return 'react-query'
          }
          return undefined
        },
      },
    },
  },
})
