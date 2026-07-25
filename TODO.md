# TODO

Agent instructions:

1. Read `SPEC.md` completely.
2. Read `PLAN.md` completely.
3. Identify the current milestone from the `Progress` section in `PLAN.md`.
4. Implement only the next incomplete milestone.
5. When milestone work is done and ready for human review, mark it as `Implemented` in `PLAN.md` even if it has not been committed yet. Treat each milestone like a merge request submitted for review.
6. Keep the change small and focused.
7. Run relevant tests before presenting results.
8. When presenting results or discussing implementation, treat the explanation as one of the most important deliverables. Do not give only a brief summary. Provide a very educational, insightful explanation with concrete examples. Cover what changed, why it exists, how it contributes to the final project goal, and how the relevant part of ADB/the code works. Explain the important protocol or code flow step by step, define project-specific terms when they first appear, include small usage or packet-flow examples where helpful, and connect the implementation to the tests so a reviewer can understand both behavior and intent. Assume the reviewer wants to learn the system, not just approve a diff.
9. Use a Conventional Commit message matching the milestone.
10. If preparing changes for human review, preset the suggested commit message by writing it to `.git/COMMIT_EDITMSG`, but do not commit unless explicitly asked.

This file is intentionally stateless. Do not track progress here; update progress only in `PLAN.md`.
