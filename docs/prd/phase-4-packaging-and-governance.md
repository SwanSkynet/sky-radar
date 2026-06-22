# Phase 4 PRD: Packaging and Governance

## Goal
Make Sky Radar genuinely usable, reviewable, and contributable by people who are not the original author — turning a working production system into a sustainable open-source project. Documentation and process are treated as deliverables here, equal in priority to code.

## Prerequisite
Phase 3 complete: the system's real reliability/scale/security posture is known and honestly documented, so the packaging produced in this phase describes something true.

## In scope
- **Public status/architecture page**: live SLO attainment (pulled from the same dashboards built in Phase 2/3, see [`../tech-stack/observability-and-ops.md`](../tech-stack/observability-and-ops.md)), current system design summary, known limitations — kept in sync with the deployed system, not a point-in-time snapshot that goes stale.
- **Complete documentation set**: root `README.md` updated to reflect the real, running system (quickstart, demo link, screenshots, links into `docs/`); `CONTRIBUTING.md` (local setup, coding standards, test expectations, PR process); `CODE_OF_CONDUCT.md`; `SECURITY.md` (already drafted in Phase 3, finalized here); runbooks for every alert defined in observability docs.
- **Postmortem template and first published postmortem**: even if Phase 1-3 ran without a real incident, a template is published and at minimum the Phase 3 DR drill is written up in that format, so the practice exists before it's needed for real.
- **Open-source governance**: license finalized and added (`LICENSE` file, per [the master PRD's governance section](../prd/00-master-prd.md)); lightweight in-repo RFC process documented and used at least once (e.g., for whatever Phase 4 decisions are still open); semantic versioning adopted for the public API and any client libraries; changelog process started.
- **Contributor on-ramp**: issues labeled for first-time contributors, a "good first issue" set drawn from real backlog items (not manufactured busywork), architecture docs cross-checked for whether someone outside the project can actually follow them.
- **Cost dashboard live and public**: monthly spend tracked against the declared budget cap from [`../tech-stack/hosting-and-deployment.md`](../tech-stack/hosting-and-deployment.md), visible alongside the SLO dashboards — consistent with treating cost as a first-class, transparent constraint rather than a private concern.

## Out of scope
- New product features (same rule as Phase 3 — this phase is about making the existing system legible and sustainable, not growing it).

## Functional / completion requirements
| ID | Requirement | Acceptance criteria |
|---|---|---|
| P4-FR1 | Status page reflects real, current SLO attainment | Spot-check: a deliberate staging degradation shows up on the page within the freshness window |
| P4-FR2 | A new contributor can go from `git clone` to a running local stack using only `README.md`/`CONTRIBUTING.md` | Dry run by someone who hasn't touched the codebase; friction points get fixed before sign-off |
| P4-FR3 | License is present and unambiguous | `LICENSE` file present, referenced from `README.md` |
| P4-FR4 | RFC process is documented and has at least one real example to point to | RFC template exists in-repo; at least one merged decision followed it |
| P4-FR5 | At least one "good first issue" is open and actually approachable | Verified by having someone other than the maintainer attempt it |
| P4-FR6 | Cost dashboard is public and matches actual billing/usage | Cross-checked against the hosting provider's own usage/billing view |

## Definition of done
- Someone who has never seen the project can: read the README, understand what it does in under a minute, see it live, find the architecture docs, and find a way to contribute — without asking the maintainer a clarifying question first.
- The project's public face (README, status page, docs) makes no claim that isn't currently true of the deployed system.
