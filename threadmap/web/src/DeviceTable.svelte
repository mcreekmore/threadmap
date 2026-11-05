<script lang="ts">
  import { role, type Device } from './types'

  let { nodes, onselect }: { nodes: Device[]; onselect: (node: Device) => void } = $props()
</script>

<div class="panel">
  <table>
    <thead>
      <tr><th>Device</th><th>Role</th><th>RLOC16</th><th>MAC</th></tr>
    </thead>
    <tbody>
      {#each nodes as node}
        <tr>
          <td><button type="button" onclick={() => onselect(node)}>{node.label}</button></td>
          <td>{role(node)}</td>
          <td><code>{node.rloc || '–'}</code></td>
          <td><code>{node.mac ?? 'unknown'}</code></td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .panel {
    overflow-x: auto;
  }

  table {
    border-collapse: collapse;
    width: 100%;
  }

  th,
  td {
    text-align: left;
    padding: var(--space-s) var(--space-m) var(--space-s) 0;
    border-bottom: 1px solid var(--line);
    white-space: nowrap;
  }

  th {
    color: var(--muted);
    font-weight: 500;
  }

  /* A plain-looking name that shows the device's card on the map. */
  button {
    all: unset;
    cursor: pointer;
  }

  button:hover {
    text-decoration: underline;
  }

  button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  code {
    font-size: var(--text-small);
  }
</style>
