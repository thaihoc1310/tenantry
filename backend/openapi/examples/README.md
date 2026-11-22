# Request and response examples

One file per operation of `../tenantry.v1.yaml`, named after its `operationId`:

```json
{ "request": { ... }, "responses": { "200": { ... }, "409": { ... } } }
```

`internal/httpapi/oapi/examples_test.go` checks every example against the
schema of its operation, and fails when an operation has no example of a
successful response (IMPLEMENTATION-PLAN §3.6). The examples show what the
API takes and sends; the integration tests check that the server does.
