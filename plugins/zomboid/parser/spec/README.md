# pzdataspec closure (world version 249)

`249/` and `common/` hold the [pzdataspec](https://github.com/cff29546/pzdataspec)
Kaitai Struct definitions that describe a Project Zomboid save, trimmed to the
closure needed to read a `localPlayers.data` player BLOB: the `iso_object`
root, its class-1 (`IsoPlayer`) path through `iso_object/`, and the
`inventory`, `visual`, `entity`, `animal`, and `item/` types those reference.
The files are copied verbatim from upstream — see `UPSTREAM.txt` for the commit
and `LICENSE.pzdataspec` for the terms. Do not edit them.

`../blob/decode.go` is a hand-written port of that closure, not generated code.
Every decode function mirrors one `.ksy` type, reads its fields in spec order,
and evaluates each `if: world_version >= N` gate for version 249; the comments
name the spec type or field wherever the mapping is not obvious. The port is
strict, so it is also a check on the closure: an out-of-range read or a single
unconsumed trailing byte fails the decode.

## Bumping the spec

A new world version never edits these files. Vendor the new upstream closure
into a sibling `spec/<version>/` directory, commit a real save blob captured
from that version under `../testdata/`, and pin the decoder's goldens to it.
Existing spec directories and fixtures stay immutable, so a version that once
decoded keeps decoding.
