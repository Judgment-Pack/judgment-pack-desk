# Staged dialect vectors

Copied byte for byte from `Judgment-Pack/judgment-pack-spec` at **a550d41**,
`conformance/dialect/vectors/`. Only each vector's `mapping.json`,
`preparation.json` and `preparation.sha256` are vendored, together with the
Runner's manifest, whose digest the specification's manifest pins. Tests check
that pin and all 30 file digests without a specification checkout.

The first nine vectors came from Runner commit
`05636eda96a9588279d61a6272625be859a28117` (PR 77), merged as
`82116bfab7705f74ca3e493c9722dd3831c7418f` with identical files. The tenth,
`http-shaped-source`, and the manifest came from Runner commit
`c84e8e64e5d30c835f8d8e6370cd48e2bfd35ee9` (PR 80), which retains the nine
unchanged. This provenance is recorded in the specification's
`conformance/dialect/README.md` at the pinned commit.

The mapping files use Runner's struct member order, not the sorted canonical
order of §1. Tests compare their canonical bytes with the previous gateway
writer and their digests with the preparations. Preparations round-trip byte
for byte in §9.5's form. Neither the fixtures nor their manifest is rewritten.

These vectors are staged, in no released specification corpus. Passing these
tests makes no preparation-verifier conformance claim.
