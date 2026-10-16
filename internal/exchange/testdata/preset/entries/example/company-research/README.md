# Example: a company's headcount, from a registry and two research recipes

**This is an example, not an entry.** It lives under `tests/fixtures/`, is in no manifest of the
Exchange, and exists so that the tests which hold a preset to its format have one to hold.

A preset of entry format version 2: a version-3 dialect without a pack and without fixtures. Its
`registry` source reads a company record through a gateway catalog binding, unbound: it names the
binding's platform and operation, the shape and the record, and no installation's profile. Its
`primary` and `secondary` sources are research recipes, unbound: they name a model and the
capabilities they need, and no AI or web profile. Its `combined` source reconciles the two, in the
order its rule declares. An installation binds every source to its own profiles and pairs the
dialect with a pack locally before any run.

The catalog checks the document's form, its digests and the scans, and nothing else: no preparation
is verified, and nothing here says the recipe is right for any pack or that any answer is correct.
