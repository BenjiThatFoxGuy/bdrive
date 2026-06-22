# Cross-repo notes

bdrive's frontend isn't in this repo. It's built and released separately at
[BenjiThatFoxGuy/bdrive-ui](https://github.com/BenjiThatFoxGuy/bdrive-ui) and pulled in at build
time via the `FRONTEND_ASSET` release URL in `taskfile.yml` (`task ui`).

Some changes require work in both repos:
- UI/frontend changes (components, styling, behavior) → bdrive-ui only.
- Backend API changes that affect the frontend (new/changed endpoints, response shapes) →
  bdrive first, then a matching change in bdrive-ui, then a new bdrive-ui release before
  bdrive can pick it up (it tracks the `latest` release tag).
- CI conventions are mirrored across both repos (e.g. `upstream-sync.yml` syncs from
  `tgdrive/teldrive` / `tgdrive/teldrive-ui` respectively) — keep them in sync when changing
  one.

When a task touches the frontend or the API contract, check/clone bdrive-ui too rather than
assuming everything lives here.
