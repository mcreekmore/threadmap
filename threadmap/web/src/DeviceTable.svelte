<script>
  let { nodes } = $props()

  function role(node) {
    if (node.kind === 'child') return 'Child'
    const roles = [node.border ? 'Border router' : 'Router']
    if (node.leader) roles.push('Leader')
    return roles.join(', ')
  }
</script>

<div class="panel">
  <table>
    <thead>
      <tr><th>Device</th><th>Role</th><th>RLOC16</th><th>MAC</th></tr>
    </thead>
    <tbody>
      {#each nodes as node (node.rloc)}
        <tr>
          <td>{node.label}</td>
          <td>{role(node)}</td>
          <td><code>{node.rloc}</code></td>
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
    padding: 6px 10px 6px 0;
    border-bottom: 1px solid var(--line);
    white-space: nowrap;
  }

  th {
    color: var(--muted);
    font-weight: 500;
  }

  code {
    font-size: 13px;
  }
</style>
