# Example: an intake request read through an unbound source, as a named artifact

**This is an example, not an entry.** It lives under `tests/fixtures/`, is in no manifest of the
Exchange, and exists so that the tests which hold an entry to its format have one to hold.

The dialect is the specification's staged `named-unbound-bound-at-import` vector
(`conformance/dialect/artifact-identity/` in the specification repository): a document in the named
format `3.0.0` whose `operation` source is unbound (RFC 0018 §4.4). The entry references the
published document, and its fixture carries `binding.json`, the binding document the fixture was
recorded under, whose `dialect` is the published document's canonical digest, identity members
included. The bound document is the fixture's, never the entry's. The specification's preparation
verifier at the commit CI pins replays the fixture to a reading.

It asserts only what the format lets an entry assert, and nothing about any record's truth.
