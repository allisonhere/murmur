---
title: ROG Flow Z13
aliases:
  - Z13
  - Flow Z13
tags:
  - linux
  - asus
  - z13
  - hardware
---

# ROG Flow Z13

Notes on running Linux on the ASUS ROG Flow Z13. Related: [[Fedora Suspend]].

## Hardware notes

- Detachable keyboard is USB HID, re-enumerates on every wake.
- GPU is handled by [[Fedora Suspend]] workarounds.

## Trackpad troubleshooting

- [ ] Confirm whether `hid_asus` binds before `hid_generic`.
  - Added: 2026-07-14

```sh
# Not a heading: this fenced block should be ignored by the indexer.
## fake heading
```

## Battery

Charge threshold lives in `/sys/class/power_supply/BAT0/charge_control_end_threshold`.
