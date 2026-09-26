# Glossary

## Authenticated identity

The user and role grants that the server reads from the verified login session.
The identity stays the same when the active context changes.

## Active context

The organization and station that one login session selects for its current work.
The server stores this context by session ID. A second login has a separate context.
The context does not add or remove role grants.

## Session

One login and its verified token. A user can have more than one session.
