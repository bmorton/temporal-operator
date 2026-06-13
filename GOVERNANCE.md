# Governance

This document describes the governance model for the temporal-operator project.

## Roles

### Contributors

Anyone who contributes code, documentation, issues, reviews, or other work to
the project. No formal process is required to become a contributor — opening a
pull request or issue is enough.

### Maintainers

Maintainers are responsible for the overall health and direction of the project.
They are listed in [MAINTAINERS.md](./MAINTAINERS.md). Responsibilities include:

- Reviewing and merging pull requests.
- Triaging issues.
- Stewarding the roadmap and release process.
- Upholding the [Code of Conduct](./CODE_OF_CONDUCT.md).

## Decision making

The project operates by **lazy consensus**. Most decisions are made implicitly:
a proposal (issue, pull request, or discussion) that receives no sustained
objection within a reasonable review window is considered approved.

- **Routine changes** (bug fixes, docs, tests) require approval from at least one
  maintainer and passing CI.
- **Significant changes** (API/CRD changes, new dependencies, architectural
  shifts) should be proposed via an issue or discussion first and require
  approval from a maintainer, with a minimum **72-hour** comment window to allow
  other maintainers to weigh in.
- **Disagreements** that cannot be resolved by lazy consensus are decided by a
  simple majority vote of the maintainers. The project lead may break ties.

## Becoming a maintainer

Contributors who have demonstrated sustained, high-quality involvement may be
nominated as maintainers by an existing maintainer. Nomination is approved by
lazy consensus of the current maintainers over a 72-hour window.

## Removing a maintainer

A maintainer may step down at any time and move to emeritus status. Inactive
maintainers (no meaningful activity for an extended period) may be moved to
emeritus by consensus of the active maintainers.

## Changes to governance

Changes to this document follow the "significant changes" process described
above.
