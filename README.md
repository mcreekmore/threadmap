# Threadmap

A Home Assistant add-on that draws a map of your Thread network: every router, its link quality, and device names where they can be found. It reads the mesh from the OpenThread Border Router add-on, so it works with HomeKit, Matter, and any other Thread device.

## Install

1. In Home Assistant, open the add-on store, choose **Repositories** from the top-right menu, and add `https://github.com/mcreekmore/threadmap`.
2. Install **Threadmap**.
3. On the add-on's Info tab, turn **Protection mode** off and start it.
4. Open **Thread map** from the sidebar.

## Security

The add-on needs Docker access to run `ot-ctl` inside the border router, and Home Assistant only grants that with Protection mode off. That setting gives the add-on broad access to your host, so stop the add-on and turn Protection mode back on when you're finished.

The page is only reachable through Home Assistant's login, with no open port. The add-on runs `meshdiag topology`, `srp server service`, `eidcache`, and `ping`, and only when you open the page or press Refresh. It never runs `dataset` commands, because their output contains the network key.

## What the map can't show yet

- Children appear only if they have an SRP name and can be located, and their link quality isn't reported.
- Sleepy devices may not answer the ping used to locate them. They're listed as warnings.
- Matter devices probably show hashed IDs, since their SRP names aren't friendly names.

## Development

```bash
cd threadmap
go test ./...
```

The fixtures in `threadmap/testdata` are sanitized captures from a real network.
