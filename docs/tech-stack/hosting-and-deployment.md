# Hosting and Deployment

See [ADR-0002](../decisions/0002-hosting-lightweight-containers.md) for why this avoids Kubernetes.

## Environments

| Environment            | Purpose                                       | Data sources                                               | Scale                                                                           |
| ---------------------- | --------------------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Local (docker-compose) | Development, the default self-host/lite mode  | Recorded fixture payloads or live calls at reduced cadence | Single instance of everything                                                   |
| Production             | The public deployment                         | Live providers                                             | Sized to [capacity targets](../architecture/system-architecture.md#scalability) |

## Hosting platform

**Primary Platform: DigitalOcean Droplet (VPS) with Docker Compose.**

The entire application runs on a single cost-effective VPS (starting at **1 vCPU and 1GB/2GB RAM**), using Docker Compose for orchestrating containers and **Caddy** for unified routing and automatic HTTPS.

**Alternative Platform: Fly.io.** (Historically configured, still possible via service `.toml` files under `/deploy/fly`).

### Rationale for Single Droplet:
1. **Low Cost:** Perfect for a portfolio project, running all services for a flat $6.00/month.
2. **Minimal Maintenance:** Docker Compose handles container restarts on failure. Caddy manages Let's Encrypt SSL certificates automatically without cron jobs or certbot scripts.
3. **Go Efficiency:** Because Go is compiled and runs with extremely low memory overhead (~15–30MB per microservice), the entire Phase 1 & Phase 2 stack runs comfortably on 1GB of RAM when paired with a 2GB Linux swap file.

---

## Deployment Topology

All production containers are managed under a single, non-exposed network. The only publicly accessible ports are **80** (HTTP) and **443** (HTTPS) owned by Caddy, which reverse-proxies inbound traffic internally:

```
                  [ Inbound Traffic (HTTPS) ]
                              |
                              v
                      Caddy (Port 80/443)
                    /                   \
                   /                     \
       (Path: /api/*, /ws)          (Path: /*)
                 v                         v
          apigateway:8080               web:8080
                 |                         |
                 v                         |
        [ Internal Network ]               |
   NATS, Redis, Postgres, Normalizer,      |
     EventEngine, Source Adapters          |
```

---

## CI/CD Pipeline (GitHub Actions)

The deployment pipeline is fully automated via GitHub Actions in [.github/workflows/ci.yml](../../.github/workflows/ci.yml):

1. **On every PR:**
   * Backend lints, vets, and tests are run (with a temporary spin-up of Postgres in CI).
   * Frontend formatting, lints, and builds are verified.
   * Every service container image (including the `web` frontend) is built to verify compilation.
2. **On merge to `main`:**
   * Docker images are built and pushed to the **GitHub Container Registry (GHCR)**, tagged with `:latest` and the unique `:${{ github.sha }}`.
   * The static frontend is built with `VITE_API_BASE_URL=/api` so it uses relative paths, making it environment-independent.
3. **Production Deploy (CD):**
   * Uses a manual approval gate configured via the GitHub **production** environment.
   * Copies the production [docker-compose.prod.yml](../../deploy/docker-compose.prod.yml) and [Caddyfile](../../deploy/Caddyfile) to the Droplet using `appleboy/scp-action`.
   * SSHs into the Droplet using `appleboy/ssh-action` to log in to GHCR, pull the latest image tags, and run `docker compose up -d --remove-orphans`.

---

## Setup Instructions

### 1. Droplet Provisioning & Preparation
Configure your Droplet with **Ubuntu 24.04 LTS** (minimum 1GB RAM recommended). 

Run the following initial commands on your Droplet to prepare the environment:

```bash
# 1. Install Docker & Compose plugin
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
apt-get update && apt-get install -y docker-compose-plugin

# 2. Setup a 2GB Swap space (crucial for 1GB RAM droplets to prevent OOM)
fallocate -l 2G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab

# 3. Create the deployment directory
mkdir -p /root/sky-radar/deploy
```

### 2. GitHub Secrets Config
In your GitHub Repository, go to **Settings > Secrets and variables > Actions** and add:
* `DIGITALOCEAN_HOST`: Your Droplet's public IP address.
* `DIGITALOCEAN_USERNAME`: Typically `root`.
* `DIGITALOCEAN_SSH_KEY`: The private SSH key matching the public key on the Droplet.
* `POSTGRES_PASSWORD`: The password for the production PostgreSQL database.
* `POSTGRES_USER`: (Optional) The username for the PostgreSQL database (defaults to `postgres`).
* `POSTGRES_DB`: (Optional) The database name for the PostgreSQL database (defaults to `postgres`).
* `POSTGRES_SSLMODE`: (Optional) The `sslmode` used by backend services connecting to PostgreSQL (defaults to `disable`, since `postgres` is only reachable over the internal Compose network). Set to `require` or stricter if PostgreSQL is moved off the internal network.
* `OPENSKY_CLIENT_ID`: OAuth2 client ID for the OpenSky Network API, used by `adapter-opensky` (e.g. `<your-opensky-account>-api-client`). Set the real value only in the secret store, not in the repo.
* `OPENSKY_CLIENT_SECRET`: OAuth2 client secret for the OpenSky Network API, used by `adapter-opensky`. **Secret — set only in GitHub Actions secrets and the droplet `.env`; never commit it to the repo.**

### 3. Ingest Coverage & Cadence

`deploy/docker-compose.prod.yml` configures the shared, server-side pollers.
Ingestion is **decoupled from any user's viewport** — these adapters continuously
fill Redis regardless of who is looking, and the frontend queries Redis by
viewport bbox. Global coverage therefore comes from running OpenSky credentialed
and global, not from any camera behaviour.

| Service | Env vars (prod compose) | Notes |
|---------|-------------------------|-------|
| `adapter-opensky` | `POLL_INTERVAL_SECONDS=120`; **no** `OPENSKY_LAMIN/LOMIN/LAMAX/LOMAX` | Fully global (`/states/all`). Credentialed via `OPENSKY_CLIENT_ID`/`OPENSKY_CLIENT_SECRET`. Carries **no** aircraft type. A global `/states/all` costs **4 OpenSky credits**; the free authenticated tier is ~4000/day, so the sustainable rate is ~one call per 86s. 120s (~2880 credits/day) stays within budget — polling faster exhausts the budget and OpenSky returns HTTP 429 (empty global coverage) until the window resets. Do **not** lower without accounting for the 4-credit global cost. |
| `adapter-adsblol` | `ADSBLOL_LAT=34.05`, `ADSBLOL_LON=-118.24`, `ADSBLOL_RADIUS_NM=250`, `POLL_INTERVAL_SECONDS=10` | Pinned to Southern California (LAX/SAN/LAS + Edwards/China Lake/Nellis). 250 NM is the provider cap. Source of aircraft type. |
| `adapter-airplaneslive` | `AIRPLANES_LIVE_LAT=34.05`, `AIRPLANES_LIVE_LON=-118.24`, `AIRPLANES_LIVE_RADIUS_NM=250`, `POLL_INTERVAL_SECONDS=10` | Same SoCal pin; 250 NM cap. Source of aircraft type. |
| `normalizer` | `MERGE_INTERVAL_SECONDS=10` | Merge cadence matched to the 10s adapter cadence. Stay within the master-PRD freshness SLO (P95 ≤ 15s). |

The SoCal pin is a deliberate high-traffic region (dense commercial + heavy
military variety within 250 NM) so the per-type/military icons are exercised. It
is a config choice — repin by overriding the `*_LAT`/`*_LON`/`*_RADIUS_NM` env
vars.

### 4. DNS Configuration
Point your domain (e.g., `skyradar.swanathiyarath.com`) to the Droplet's IP address. Caddy will detect the request, secure a Let's Encrypt certificate, and handle HTTPS traffic automatically.
