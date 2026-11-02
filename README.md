# Threadmap

A Home Assistant add-on that draws a map of your Thread network: every router, its link quality, and device names where they can be found. It reads the mesh from the OpenThread Border Router add-on, so it works with HomeKit, Matter, and any other Thread device.

## Install

1. In Home Assistant, open the add-on store, choose **Repositories** from the top-right menu, and add `https://github.com/mcreekmore/threadmap`.
2. Install **Threadmap**.
3. On the add-on's Info tab, turn **Protection mode** off and start it.
4. Open **Thread map** from the sidebar.

## Security

The add-on needs Docker access to run `ot-ctl` inside the border router, and Home Assistant only grants that with Protection mode off. That setting gives the add-on broad access to your host, so stop the add-on and turn Protection mode back on when you're finished.

The page is only reachable through Home Assistant's login, with no open port. The add-on runs `meshdiag topology children`, `meshdiag topology`, `srp server service`, `eidcache`, and `ping`, and only when you open the page or press Refresh. It never runs `dataset` commands, because their output contains the network key.

## What the map can't show yet

- Children come from each router's child table, which doesn't report MACs. An unnamed child is labelled by its RLOC16.
- A sleepy device may not answer the ping used to find its name, so it can appear under its RLOC16, with a warning that its name wasn't located.
- A stale address cache entry can show a name on the wrong device until the entry expires.
- Matter devices probably show hashed IDs, since their SRP names aren't friendly names.

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
