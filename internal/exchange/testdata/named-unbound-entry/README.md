# Example entry for a named artifact with an unbound source

**An example, not an entry.** `entries/example/fx-named-unbound/` here is laid out as an entry of
entry format version 3 for a named-format document with an unbound source is, its fixture holding
`binding.json`, with its own `manifest.json` beside it. Nothing here is published by the Exchange,
and the repository root's `manifest.json` does not list it.

Every identifier, record and key in this tree is synthetic, and no live service was asked: the
records and receipts are those of the specification's staged vectors, written by the Runner's
tests against a stand-in of a gateway in the test's own process and signed with keys made for that
run.
