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
```

### 2. GitHub Secrets Config
In your GitHub Repository, go to **Settings > Secrets and variables > Actions** and add:
* `DIGITALOCEAN_HOST`: Your Droplet's public IP address.
* `DIGITALOCEAN_USERNAME`: Typically `root`.
* `DIGITALOCEAN_SSH_KEY`: The private SSH key matching the public key on the Droplet.

### 3. DNS Configuration
Point your domain (e.g., `skyradar.swanathiyarath.com`) to the Droplet's IP address. Caddy will detect the request, secure a Let's Encrypt certificate, and handle HTTPS traffic automatically.
