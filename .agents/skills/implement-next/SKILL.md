---
name: implement-next
description: Implements the next incomplete project milestone from docs/init/PLAN.md using docs/init/SPEC.md as the requirements source. Use when the user asks to implement the next milestone, continue the plan, or use the implement-next skill.
---

# Implement Next Milestone

Follow this workflow exactly:

1. Read `docs/init/SPEC.md` completely.
2. Read `docs/init/PLAN.md` completely.
3. Identify the current milestone from the `Progress` section in `docs/init/PLAN.md`.
4. Implement only the next incomplete milestone.
5. When milestone work is complete, tests have passed, and the milestone has been committed, mark it as `Done` in `docs/init/PLAN.md`.
6. Keep the change small and focused.
7. Run relevant tests before presenting results.
8. When presenting results or discussing implementation, treat the explanation as one of the most important deliverables. Do not give only a brief summary. Provide a very educational, insightful explanation with concrete examples. Cover what changed, why it exists, how it contributes to the final project goal, and how the relevant part of ADB/the code works. Explain the important protocol or code flow step by step, define project-specific terms when they first appear, include small usage or packet-flow examples where helpful, and connect the implementation to the tests so a reviewer can understand both behavior and intent. Assume the reviewer wants to learn the system, not just approve a diff.
9. Use a Conventional Commit message matching the milestone.
10. When a completed milestone is ready for human review, commit it automatically using the milestone's Conventional Commit message. Then update its status to `Done` in `docs/init/PLAN.md` and include that status update in the same commit. The human will review the resulting commit instead of reviewing uncommitted changes.
11. After finishing and committing one milestone, briefly identify the next incomplete milestone by number, title, status, commit message, and short purpose, then offer to continue. If there is no next active incomplete milestone, say that explicitly and mention whether only deferred milestones remain.

This skill is intentionally stateless. Do not track progress here; update progress only in `docs/init/PLAN.md`.
