# ADR 0001: Authenticated identity and active context

## Status

Accepted.

## Decision

Keep authenticated identity and active context separate.

The identity contains the user and role grants. The server reads it from the verified session.
A context change does not change the identity or its role grants.

Store the active organization and station against the login session ID.
Accept an existing organization and station in one CSRF-protected request.
The station must belong to the organization. The target records may be disabled.
A disabled scope is read-only for operational work. Reads remain available for
historical review. Reject each operational write with `org_disabled` or
`station_disabled` as an HTTP 409 conflict.

Use the active organization and station for scoped requests when the session has a context.
Keep current scope rules when the session has no context. Allow a Superadmin to
re-enable an organization or station through its administration endpoint.

## Consequences

Each login session can select a different context.
A session read returns the active context, or `null` when no context is set.

## Saved context preference

Save the latest context change for the account. Read this preference after the
server issues a new login token. Seed only the new session with the preference.
Do not change an active session when another session changes its context.

Use the saved organization and station when both records exist and the station
belongs to that organization. Do not require enabled targets. A disabled target
is still valid for historical reads.

If the saved organization or station is invalid, leave the new session without
an active context. Use the existing identity defaults for that session. Do not
choose a station in this fallback. This keeps identity and context separate.
