# Protocol versioning

The Go import path and HTTP base path carry the major wire version:

```text
github.com/CytieqLabs/Bifrost/protocol/v1
/v1
```

Within v1, releases may add optional response fields, new endpoints, new event
types, or new optional request fields. Implementations must ignore unknown
response and event fields. Servers reject unknown request fields so caller
mistakes fail visibly.

A new major version is required to remove or rename a field, change a field's
meaning or JSON type, make an optional field mandatory, change endpoint
semantics incompatibly, or reuse an enum value for different behavior.

Protocol releases use semantic tags. Bifrost Client pins a released module
version; the shared development workspace may temporarily resolve the sibling
module before that tag exists.
