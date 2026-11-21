import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// En développement (npm run dev), Vite sert l'application avec rechargement à
// chaud, et relaie /api vers le BFF : les mêmes chemins qu'en production, où
// le BFF sert l'application lui-même (embarquée dans le binaire).
export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 5173,
    proxy: { '/api': 'http://localhost:8080' },
  },
  build: {
    outDir: 'dist',
    // Les fichiers produits portent une empreinte de leur contenu
    // (assets/index-3f2a1b.js) : le BFF peut les mettre en cache un an.
    assetsDir: 'assets',
  },
})
