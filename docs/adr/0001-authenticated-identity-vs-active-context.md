# ADR 0001: Authenticated identity and active context

## Status

Accepted.

## Decision

Keep authenticated identity and active context separate.

The identity contains the user and role grants. The server reads it from the verified session.
A context change does not change the identity or its role grants.

Store the active organization and station against the login session ID.
Allow a Superadmin to set both values in one CSRF-protected request.
Accept only an enabled organization and an enabled station in that organization.

Use the active organization and station for scoped requests when the session has a context.
Keep current scope rules when the session has no context.

## Consequences

Each login session can select a different context.
A session read returns the active context, or `null` when no context is set.
