# ADR-0002: Lightweight containers, no Kubernetes

## Status
Accepted

## Context
Sky Radar has no revenue and is operated by volunteer maintainers. Hosting candidates considered: managed Kubernetes (GKE/EKS Autopilot or similar), and lightweight container hosting (a single small VPS or a PaaS such as Fly.io/Railway running Docker-Compose-style services).

## Decision
Sky Radar is deployed as a small number of containerized services on a lightweight PaaS (Fly.io, primary candidate — see [`hosting-and-deployment.md`](../tech-stack/hosting-and-deployment.md) for the full evaluation), without Kubernetes.

## Rationale
- **Operational load matches who's actually operating it.** A volunteer on-call rotation cannot absorb Kubernetes's operational surface (cluster upgrades, node pool management, ingress/cert lifecycle, RBAC) on top of the product itself. Every hour spent on cluster ops is an hour not spent on the system this project exists to demonstrate.
- **Cost predictability.** Kubernetes control planes and node pools on managed cloud providers either cost money outright or require constant attention to stay inside a free tier; a small number of always-on lightweight VMs/containers is dramatically easier to reason about and cap.
- **Sky Radar's actual scale doesn't need it.** At the target scale (tens of thousands of tracked aircraft, see [`docs/architecture/system-architecture.md`](../architecture/system-architecture.md)), a handful of Go services and a couple of data stores comfortably fit on 2-4 small instances. Kubernetes earns its complexity at a scale and team size Sky Radar doesn't have.
- **Faster path to a real, running, production system.** Given the project's own goal ("ship a real end-to-end pipeline before advanced features," [phase-1-foundation.md](../prd/phase-1-foundation.md)), time-to-first-deploy matters; a PaaS deploy is hours, a hardened Kubernetes setup is weeks.

## Rejected alternative
- **Managed Kubernetes** — a stronger individual signal of "I can operate Kubernetes," but a worse fit for this project's actual operating model and budget. Documented here so the trade-off is explicit rather than implicitly "we just didn't get to it" — this is reconsiderable via RFC if the project's scale or maintainer capacity changes materially.

## Consequences
- No Terraform-managed Kubernetes manifests; infra-as-code targets the chosen PaaS's config format (e.g., `fly.toml`) plus Terraform only for ancillary cloud resources (DNS, object storage) that the PaaS doesn't manage itself.
- Horizontal scaling is "add another small instance," not pod autoscaling — acceptable given the capacity ceiling in [`system-architecture.md`](../architecture/system-architecture.md#scalability).
- A self-host/"lite mode" (single instance, docker-compose, reduced provider set) remains the local dev environment and the path for anyone who wants to run their own copy without needing this hosting setup at all.
