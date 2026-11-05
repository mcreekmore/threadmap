<script lang="ts">
  import { qualityName, role, type Device, type Link } from './types'

  let { device, nodes, links }: { device: Device; nodes: Device[]; links: Link[] } = $props()

  // Each neighbour seen from this device: "out" is how well the neighbour hears it, "in" how well it hears the neighbour.
  const neighbours = $derived(
    links
      .filter((l) => l.a === device.rloc || l.b === device.rloc)
      .map((l) => {
        const isA = l.a === device.rloc
        return {
          device: nodes.find((n) => n.rloc === (isA ? l.b : l.a)),
          quality: l.quality,
          out: isA ? l.aToB : l.bToA,
          in: isA ? l.bToA : l.aToB,
          margin: l.margin,
        }
      }),
  )
  const parent = $derived(device.kind === 'child' ? neighbours[0]?.device : undefined)
  const childCount = $derived(neighbours.filter((n) => n.device?.kind === 'child').length)
  const product = $derived([device.ha?.manufacturer, device.ha?.model].filter(Boolean).join(' '))

  function linkQuality(n: (typeof neighbours)[number]) {
    if (n.margin !== undefined) return `${qualityName(n.quality)}, ${n.margin} dB`
    if (n.out === undefined || n.in === undefined || n.out === n.in) return qualityName(n.quality)
    return `${qualityName(n.out)} out, ${qualityName(n.in)} in`
  }
</script>

<section class="card" aria-label="Details for {device.label}">
  <h2>{device.label}</h2>
  <p class="muted">{role(device)}{device.ha?.area ? ` · ${device.ha.area}` : ''}</p>

  {#if device.kind === 'missing'}
    <p class="note">Registered, but no router has it in the mesh right now. It may be asleep or offline.</p>
  {/if}

  {#if product || device.ha?.firmware}
    <dl>
      {#if product}<dt>Model</dt><dd>{product}</dd>{/if}
      {#if device.ha?.firmware}<dt>Firmware</dt><dd>{device.ha.firmware}</dd>{/if}
    </dl>
  {/if}

  {#if parent || childCount > 0}
    <dl>
      {#if parent}<dt>Parent</dt><dd>{parent.label}</dd>{/if}
      {#if childCount > 0}<dt>Children</dt><dd>{childCount}</dd>{/if}
    </dl>
  {/if}

  {#if neighbours.length > 0}
    <h3 title="Out is how well the other device hears this one; in is how well this one hears it">Links</h3>
    <ul>
      {#each neighbours as n}
        <li><span>{n.device?.label ?? 'Unknown'}</span> <span class="muted">{linkQuality(n)}</span></li>
      {/each}
    </ul>
  {/if}

  <dl class="ids">
    {#if device.rloc}<dt>RLOC16</dt><dd><code>{device.rloc}</code></dd>{/if}
    {#if device.mac}<dt>MAC</dt><dd><code>{device.mac}</code></dd>{/if}
    {#if device.omr}<dt>IPv6</dt><dd><code>{device.omr}</code></dd>{/if}
  </dl>

  {#if device.ha}
    <!-- The page runs inside HA's ingress frame, so open the device page in HA itself. -->
    <a href="/config/devices/device/{device.ha.id}" target="_top">Open in Home Assistant</a>
  {/if}
</section>

<style>
  .card {
    padding: var(--space-m);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 4px 16px rgb(0 0 0 / 0.2);
    font-size: var(--text-small);
  }

  h2 {
    font-size: 15px;
    font-weight: 500;
    margin: 0;
    overflow-wrap: anywhere;
  }

  h3 {
    font-size: var(--text-small);
    font-weight: 500;
    margin: var(--space-m) 0 var(--space-s);
  }

  p {
    margin: 0;
  }

  .note {
    margin-top: var(--space-m);
  }

  .muted {
    color: var(--muted);
  }

  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px var(--space-m);
    margin: var(--space-m) 0 0;
  }

  dt {
    color: var(--muted);
  }

  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  li {
    display: flex;
    justify-content: space-between;
    gap: var(--space-m);
  }

  a {
    display: inline-block;
    margin-top: var(--space-m);
    color: var(--accent);
  }
</style>
