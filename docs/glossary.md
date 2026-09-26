# Glossary

## Authenticated identity

The user and role grants that the server reads from the verified login session.
The identity stays the same when the active context changes.

## Active context

The organization and station that one login session selects for its current work.
The server stores this context by session ID. A second login has a separate context.
The context does not add or remove role grants.

## Saved context preference

The latest context that a Superadmin selects for one account.
A new login uses this context when its organization and station still exist.
Each session keeps its own context after login.
An invalid preference leaves the new session without an active context.
The identity defaults then apply.

## Session

One login and its verified token. A user can have more than one session.

## Disabled scope

An organization or station with `enabled` set to `false`.
The server permits reads in this scope for historical review.
The server rejects operational writes in this scope.
A Superadmin can re-enable the organization or station.
