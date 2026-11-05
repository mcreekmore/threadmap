<script lang="ts">
  import cytoscape from 'cytoscape'
  import DeviceCard from './DeviceCard.svelte'
  import { nodeID, role, type Device, type Link } from './types'

  let { nodes, links }: { nodes: Device[]; links: Link[] } = $props()
  let container: HTMLDivElement

  // The device whose card is showing. Hovering shows it; tapping pins it so its link can be clicked.
  let selected = $state<string | null>(null)
  let pinned = false
  let anchor = $state({ x: 0, y: 0 }) // the selected node's position on screen
  let wrapWidth = $state(0)
  let graphHeight = $state(0)
  let cardHeight = $state(0)
  const device = $derived(nodes.find((n) => nodeID(n) === selected))
  const cardWidth = $derived(Math.min(280, wrapWidth - 16))
  // Beside the node, kept inside the graph's width; above the node when there's no room below it.
  const cardLeft = $derived(Math.max(8, Math.min(anchor.x + 20, wrapWidth - cardWidth - 8)))
  const cardTop = $derived(anchor.y + 20 + cardHeight > graphHeight ? Math.max(8, anchor.y - 20 - cardHeight) : anchor.y + 20)

  // Kept across refreshes so the map doesn't rearrange itself or undo your dragging.
  const positions = new Map<string, cytoscape.Position>()

  function elements() {
    const children = new Set(nodes.filter((n) => n.kind === 'child').map((n) => n.rloc))
    // An unnamed border router is labelled "Border router", which its role already says.
    const text = (n: Device) => (role(n).startsWith(n.label) ? role(n) : `${n.label}\n${role(n)}`)
    return [
      ...nodes.map((n) => ({
        data: { id: nodeID(n), text: text(n), kind: n.kind, border: n.border === true },
        position: positions.get(nodeID(n)),
      })),
      ...links.map((l) => ({
        data: {
          source: l.a,
          target: l.b,
          quality: l.quality,
          text: `q${l.quality}`,
          toChild: children.has(l.a) || children.has(l.b),
        },
      })),
    ]
  }

  // Canvas styles can't read CSS variables, so resolve them to real colours.
  function graphStyle(): cytoscape.StylesheetJson {
    const css = getComputedStyle(document.documentElement)
    const color = (name: string) => css.getPropertyValue(name).trim()
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
      { selector: 'node[kind = "missing"]', style: { width: 22, height: 22, opacity: 0.5, 'border-style': 'dashed' } },
      {
        selector: 'edge',
        style: {
          width: 3,
          label: 'data(text)',
          'font-size': 12,
          color: color('--muted'),
          'text-outline-color': color('--panel'),
          'text-outline-width': 3,
        },
      },
      { selector: 'edge[?toChild]', style: { 'line-style': 'dashed' } },
      { selector: 'edge[quality = 3]', style: { 'line-color': color('--q3') } },
      { selector: 'edge[quality = 2]', style: { 'line-color': color('--q2') } },
      { selector: 'edge[quality = 1]', style: { 'line-color': color('--q1') } },
      { selector: 'edge[quality = 0]', style: { 'line-color': color('--q0') } },
    ]
  }

  function layout() {
    if (nodes.every((n) => n.kind === 'missing' || positions.has(nodeID(n)))) return { name: 'preset', padding: 50 }
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
      layout: { name: 'preset' }, // Keeps the given positions; the real layout runs once the graph is ready.
      maxZoom: 1.25,
    })
    // Missing devices have no links, so the layout would push them far out and shrink the mesh to fit; row them up under it.
    cy.ready(() => {
      const missing = cy.nodes('[kind = "missing"]')
      const mesh = cy.elements().not(missing)
      mesh.layout(layout()).run()
      const below = mesh.boundingBox()
      missing.forEach((n, i) => {
        if (!positions.has(n.id())) n.position({ x: below.x1 + i * 140, y: below.y2 + 60 })
      })
      cy.fit(undefined, 50)
    })

    const show = (node: cytoscape.NodeSingular) => {
      selected = node.id()
      anchor = { ...node.renderedPosition() }
    }
    cy.on('mouseover', 'node', (e) => !pinned && show(e.target))
    cy.on('mouseout', 'node', () => !pinned && (selected = null))
    cy.on('tap', 'node', (e) => {
      pinned = true
      show(e.target)
    })
    cy.on('tap', (e) => {
      if (e.target === cy) {
        pinned = false
        selected = null
      }
    })
    // Keep the card next to its node while the graph is panned, zoomed or the node dragged.
    cy.on('viewport position', () => {
      if (selected) show(cy.getElementById(selected))
    })

    const dark = window.matchMedia('(prefers-color-scheme: dark)')
    const restyle = () => cy.style(graphStyle())
    dark.addEventListener('change', restyle)

    return () => {
      selected = null
      pinned = false
      dark.removeEventListener('change', restyle)
      cy.nodes().forEach((n) => {
        positions.set(n.id(), { ...n.position() })
      })
      cy.destroy()
    }
  })
</script>

<div class="wrap" bind:clientWidth={wrapWidth}>
  <div
    class="panel graph"
    bind:this={container}
    bind:clientHeight={graphHeight}
    onmouseleave={() => !pinned && (selected = null)}
    role="img"
    aria-label="Diagram of Thread devices and their links; the same devices are listed in the table below"
  ></div>
  {#if device}
    <div class="popup" style:left="{cardLeft}px" style:top="{cardTop}px" style:width="{cardWidth}px" bind:clientHeight={cardHeight}>
      <DeviceCard {device} {nodes} {links} />
    </div>
  {/if}
</div>

<style>
  .wrap {
    position: relative;
  }

  .graph {
    height: min(62vh, 560px);
    padding: 0;
  }

  .popup {
    position: absolute;
    z-index: 1;
  }
</style>
