<script>
  import cytoscape from 'cytoscape'

  let { nodes, links } = $props()
  let container

  // Kept across refreshes so the map doesn't rearrange itself or undo your dragging.
  const positions = new Map()

  function elements() {
    return [
      ...nodes.map((n) => ({
        data: { id: n.rloc, text: n.leader ? `${n.label}\nLeader` : n.label, kind: n.kind, border: n.border === true },
        position: positions.get(n.rloc),
      })),
      ...links.map((l) => ({
        data: { source: l.a, target: l.b, quality: l.quality ?? undefined, text: l.quality === null ? '' : `q${l.quality}` },
      })),
    ]
  }

  // Canvas styles can't read CSS variables, so resolve them to real colours.
  function graphStyle() {
    const css = getComputedStyle(document.documentElement)
    const color = (name) => css.getPropertyValue(name).trim()
    return [
      {
        selector: 'node',
        style: {
          width: 32,
          height: 32,
          'background-color': color('--panel'),
          'border-width': 2.5,
          'border-color': color('--muted'),
          label: 'data(text)',
          'text-wrap': 'wrap',
          'text-valign': 'bottom',
          'text-margin-y': 6,
          'font-size': 13,
          color: color('--text'),
          'text-outline-color': color('--panel'),
          'text-outline-width': 2,
        },
      },
      { selector: 'node[?border]', style: { width: 40, height: 40, 'background-color': color('--accent'), 'border-color': color('--accent') } },
      { selector: 'node[kind = "child"]', style: { width: 22, height: 22 } },
      {
        selector: 'edge',
        style: {
          width: 3,
          'line-color': color('--unknown'),
          'line-style': 'dashed',
          label: 'data(text)',
          'font-size': 12,
          color: color('--muted'),
          'text-outline-color': color('--panel'),
          'text-outline-width': 3,
        },
      },
      { selector: 'edge[quality]', style: { 'line-style': 'solid' } },
      { selector: 'edge[quality = 3]', style: { 'line-color': color('--q3') } },
      { selector: 'edge[quality = 2]', style: { 'line-color': color('--q2') } },
      { selector: 'edge[quality = 1]', style: { 'line-color': color('--q1') } },
      { selector: 'edge[quality = 0]', style: { 'line-color': color('--q0') } },
    ]
  }

  function layout() {
    if (nodes.every((n) => positions.has(n.rloc))) return { name: 'preset', padding: 50 }
    return {
      name: 'cose',
      animate: false,
      padding: 50,
      randomize: positions.size === 0,
      nodeDimensionsIncludeLabels: true,
      idealEdgeLength: () => 140,
      nodeRepulsion: () => 400000,
    }
  }

  // Rebuilds the graph whenever nodes or links change.
  $effect(() => {
    const cy = cytoscape({
      container,
      elements: elements(),
      style: graphStyle(),
      layout: layout(),
      maxZoom: 1.25,
    })

    const dark = window.matchMedia('(prefers-color-scheme: dark)')
    const restyle = () => cy.style(graphStyle())
    dark.addEventListener('change', restyle)

    return () => {
      dark.removeEventListener('change', restyle)
      cy.nodes().forEach((n) => positions.set(n.id(), { ...n.position() }))
      cy.destroy()
    }
  })
</script>

<div
  class="panel graph"
  bind:this={container}
  role="img"
  aria-label="Diagram of Thread devices and their links; the same devices are listed in the table below"
></div>

<style>
  .graph {
    height: min(62vh, 560px);
    padding: 0;
  }
</style>
