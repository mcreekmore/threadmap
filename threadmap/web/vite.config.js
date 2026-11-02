import { readFileSync } from 'node:fs'
import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// Lets `npm run dev` work without Home Assistant or a Thread network.
const sampleTopology = {
  name: 'sample-topology',
  configureServer(server) {
    server.middlewares.use('/api/topology', (_req, res) => {
      res.setHeader('Content-Type', 'application/json')
      res.end(readFileSync('dev/topology.json'))
    })
  },
}

export default defineConfig({
  // Relative asset URLs: under ingress the page lives below a per-session path.
  base: './',
  plugins: [svelte(), sampleTopology],
})
