<script lang="ts">
  import DeviceTable from './DeviceTable.svelte'
  import MeshGraph from './MeshGraph.svelte'
  import { qualities, type MeshMap } from './types'

  const savedKey = 'threadmap-last-map'

  function lastMap(): MeshMap | null {
    try {
      return JSON.parse(localStorage.getItem(savedKey) ?? 'null')
    } catch {
      return null
    }
  }

  let map = $state(lastMap())
  let error = $state('')
  let status = $state('')
  let scanning = $state(false)

  async function refresh() {
    scanning = true
    error = ''
    status = map ? 'Showing the last scan, updating…' : 'Scanning the mesh…'
    try {
      // Relative URL: under ingress the page lives below a per-session path.
      const res = await fetch('api/topology', { cache: 'no-store' })
      // While the add-on restarts, ingress answers with an HTML error page.
      const data = await res.json().catch(() => null)
      if (!res.ok || !data)
        throw new Error(
          data?.error ?? `Scan failed: ${res.status} ${res.statusText}`,
        )
      map = data
      try {
        localStorage.setItem(savedKey, JSON.stringify(data))
      } catch {
        // Without storage, the next load just waits for its scan.
      }
      const count = data.nodes.length
      status = `${count} device${count === 1 ? '' : 's'}, scanned ${new Date().toLocaleTimeString()}`
    } catch (e) {
      error = (e as Error).message
      status = map ? 'Scan failed, showing the last scan' : 'Scan failed'
    } finally {
      scanning = false
    }
  }

  refresh()
</script>

<main>
  <header>
    <h1>Thread map</h1>
    <span class="status" role="status">{status}</span>
    <button
      type="button"
      onclick={refresh}
      disabled={scanning}
      aria-label="Refresh"
      title="Refresh">↻</button
    >
  </header>

  {#if error}
    <p class="panel error" role="alert">{error}</p>
  {/if}

  {#if map}
    <MeshGraph nodes={map.nodes} links={map.links} />

    <div class="legend">
      {#each qualities as { level, name, meaning }}
        <span title={meaning}
          ><i style:border-color="var(--q{level})"></i>q{level} {name}</span
        >
      {/each}
    </div>

    {#if map.warnings.length > 0}
      <ul class="panel warnings">
        {#each map.warnings as warning}
          <li>{warning}</li>
        {/each}
      </ul>
    {/if}

    <DeviceTable nodes={map.nodes} />
  {/if}
</main>

<style>
  main {
    max-width: 1100px;
    margin: auto;
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-l);
  }

  header {
    display: flex;
    align-items: center;
    gap: var(--space-m);
  }

  h1 {
    font-size: var(--text-large);
    font-weight: 400;
    margin: 0;
  }

  .status {
    color: var(--muted);
    font-size: var(--text-small);
    flex: 1;
    min-width: 0;
  }

  h1,
  button {
    flex-shrink: 0;
  }

  button {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    padding: 0;
    font-size: var(--text-large);
    line-height: 1;
    color: var(--text);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 50%;
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.6;
    cursor: progress;
  }

  button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .error {
    color: var(--q0);
    margin: 0;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-s) var(--space-m);
    color: var(--muted);
    font-size: var(--text-small);
  }

  .legend i {
    display: inline-block;
    width: 14px;
    border-top: 3px solid;
    vertical-align: middle;
    margin-right: var(--space-s);
  }

  .warnings {
    margin: 0;
    padding-left: 32px;
  }
</style>
