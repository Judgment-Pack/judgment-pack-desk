# Example: currency conversion on an invoice

**This is an example, not an entry.** It lives under `tests/fixtures/`, is in no manifest of the
Exchange, and exists so that the tests which hold an entry to its format have one to hold.

The dialect is the specification's staged `valid` preparation vector (`conformance/dialect/vectors/valid`
in the specification repository): a version-2 dialect with one `operation` source that asks a
gateway tool to convert an invoice's amount, and copies one fact and two evidence requirements
from the case. Its fixture is the vector's recorded records and its `preparation.json`, byte for
byte.

It asserts only what the format lets an entry assert, and nothing about any record's truth.
