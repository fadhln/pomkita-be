# Issue tracker: Plane

Issues for both Pomkita repos live in Plane workspace `fadhln-workbench`, project `POM / pomkita` (project ID `9dcc69fb-06a9-40a1-ab5b-22b372e16b5d`).

Use the connected Plane tools for issue operations.

- Create a work item with `mcp__plane__workitem` action `create` and this project ID.
- Read a work item by its key, such as `POM-12`, with `retrieve_by_identifier`; use `retrieve` when you have its ID.
- List work items with `list` and this project ID.
- Add comments with `mcp__plane__workitem_comment` action `create`.
- Change status by resolving the state ID by name with `mcp__plane__state`, then using `workitem` action `update`. Current states are Backlog, Todo, In Progress, Done, and Cancelled.
- Add blockers with `mcp__plane__workitem_relation` action `create` and relation type `blocked_by`.

For Wayfinding, use a parent work item for the map and set each child work item's `parent` to the map's ID. Use native `blocked_by` relations for blocking edges.

Refer to work items by their Plane key, such as `POM-12`.
