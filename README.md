# Threadmap

A Home Assistant add-on that draws a map of your Thread network: every router, its link quality, and device names where they can be found. It reads the mesh from the OpenThread Border Router add-on, so it works with HomeKit, Matter, and any other Thread device.

## Install

1. In Home Assistant, open the add-on store, choose **Repositories** from the top-right menu, and add `https://github.com/mcreekmore/threadmap`.
2. Install **Threadmap**.
3. Start it, leaving **Protection mode** on.
4. Open **Thread map** from the sidebar.

If you installed a version before the switch to the REST API, turn Protection mode back on: it no longer needs to be off.

## Security

The add-on has no Docker access. It reads the mesh from the border router's REST API, which Home Assistant keeps on its internal network, so the REST API port can stay unset. That API has no login, so anything on the internal network can reach it, including routes that expose the network key or factory reset the border router. Threadmap only uses `/api/actions`, `/api/devices` and `/api/diagnostics`.

It uses host networking to read device names over mDNS. The page listens on Home Assistant's internal interface and only accepts requests from its login-protected ingress gateway, so it has no open port on your LAN. Scans only run when you open the page or press Refresh.

## What the map can't show yet

- Device names come from mDNS, matched by each device's IPv6 address. A device that advertises a different address shows its MAC, with a warning if it looks like it's on the mesh.
- Matter devices probably show hashed IDs, since their service names aren't friendly names.
- Only the border router the add-on talks to is marked as a border router.
- Child devices haven't been tested on a real network yet.

## Development

The backend is Go with no dependencies. The page is Svelte 5, Vite and Cytoscape, in `threadmap/web`.

```bash
cd threadmap/web
npm ci
npm run dev        # live reload with sample data from dev/topology.json
npm run build      # required before Go builds, since the binary embeds web/dist

cd ..
go test ./...
```

The fixtures in `threadmap/testdata` are sanitized captures from a real network.

## Releasing

Push a version tag from `main`:

```bash
git tag v0.2.0
git push origin v0.2.0
```

GitHub Actions runs the tests, publishes the `ghcr.io/mcreekmore/threadmap` image for amd64 and arm64, sets `version` in `config.yaml`, and creates the GitHub release. Don't change `version` by hand: Home Assistant offers the update as soon as it changes, so the image has to exist first.

## AI disclosure

This project was written with the help of AI tools.
