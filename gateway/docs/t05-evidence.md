# T05 native subject namespace — 2026-10-03

The optional `trustedSubjectRequests` runtime setting accepts
`X-Connector-Subject` only after an actual administrator bearer is verified.
The server entrypoint enables it only when
`OOMOL_CONNECT_TRUSTED_SUBJECT_REQUESTS=true`. It defaults to disabled;
without the header, legacy `local-admin` behavior remains unchanged.

The trusted caller supplies a stable opaque identifier scoped to its project
and user. Use an unambiguous encoding or digest of that tuple, not ambiguous
string concatenation. The accepted value is 1–128 ASCII letters, digits,
underscores, dots, colons or hyphens. The stored owner has a separate
`trusted-subject:` namespace. Invalid/disabled context is rejected explicitly.
Browser cookies and ordinary runtime credentials cannot assert it.

Create, reconnect, poll and server-side SaaS poll sync share the same owner.
Native callbacks use stored state; callback parameters cannot override owner.
This patch namespaces OAuth transactions only. It does not make the native
connection store multitenant or authorize arbitrary connection access. The
future gateway must still enforce ownership and exact runtime grants.
The hosted SaaS browser completion route continues to use local-admin;
that route is outside this native-only integration and is not claimed supported.

Evidence: two subjects can start the same provider without cancelling each
other, cross-subject/project polls return 404, the actual native callback
completes only its transaction, reconnect retains the same namespace, and
runtime/cookie/invalid/disabled-header cases are rejected. Legacy no-header
creation still works. Related connection/auth/runtime Vitest tests and
`npm run fix-check` pass. The provider exchange is a local fixture, not live
OAuth, and no Multica browser verification is implemented yet.

Independent read-only reviewer `/root/review_t02` found no specific security
defect in this native scope and highlighted the excluded SaaS browser route.
No PR was created because remote changes are prohibited in this session.
