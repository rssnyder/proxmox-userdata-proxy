# Proxmox Userdata Proxy

A transparent API proxy for Proxmox VE that adds support for inline cloud-init user-data at VM creation time.

docker image avalible at `rssnyder/proxmox-userdata-proxy`

## Problem

Proxmox VE requires cloud-init snippets to exist on storage before VM creation. There is no API endpoint to upload snippets - they must be created via filesystem access. This makes automation difficult.

## Solution

This proxy sits between your automation tooling and the Proxmox API. It accepts standard Proxmox API calls with two additional fields:

- `cloudinit_userdata` - Full cloud-init YAML content
- `cloudinit_storage` - Storage ID where the snippet will be written

The proxy writes the snippet to the storage filesystem and forwards the modified request to Proxmox with the `cicustom` parameter set.

## Requirements

- Proxmox VE 7.x or 8.x
- Storage with snippets support (NFS recommended for clusters)
- The storage must be mounted to the proxy container

## Quick Start

1. **Create a Proxmox API token:**

```bash
pveum user add automation@pve
pveum aclmod / -user automation@pve -role PVEVMAdmin
pveum user token add automation@pve proxy --privsep=0
```

2. **Mount your storage:**

If using NFS, mount it on the Docker host:
```bash
mount -t nfs proxmox-nfs:/storage /mnt/pve/nfs-storage
```

3. **Configure and run:**

```bash
# Edit docker-compose.yml with your settings
docker compose up -d
```

## Configuration

| Environment Variable | Required | Default | Description |
|---------------------|----------|---------|-------------|
| `PROXMOX_URL` | Yes | - | Proxmox API URL (e.g., `https://pve.local:8006`) |
| `PROXMOX_INSECURE` | No | `false` | Skip TLS verification for Proxmox |
| `PROXY_LISTEN_ADDR` | No | `:8443` | Address to listen on |
| `SNIPPET_STORAGE_MAP` | Yes | - | Storage ID to path mapping (e.g., `local=/var/lib/vz,nfs=/mnt/nfs`) |
| `SNIPPET_STORAGE_PATH` | No | - | Shorthand for `local=<path>` |
| `LOG_LEVEL` | No | `info` | Log level (debug, info, warn, error) |
| `TLS_CERT_FILE` | No | - | Path to TLS certificate |
| `TLS_KEY_FILE` | No | - | Path to TLS key |

## Usage

### Create VM with Cloud-Init

Send a standard Proxmox VM creation request with the additional cloud-init fields:

```bash
curl -X POST "http://localhost:8443/api2/json/nodes/pve/qemu" \
  -H "Authorization: <Proxmox Auth Header>" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "vmid=100" \
  -d "name=my-vm" \
  -d "memory=2048" \
  -d "cores=2" \
  -d "net0=virtio,bridge=vmbr0" \
  -d "scsi0=local-lvm:32" \
  -d "cloudinit_storage=nfs-storage" \
  -d "cloudinit_userdata=#cloud-config
package_upgrade: true
packages:
  - qemu-guest-agent
  - vim
runcmd:
  - systemctl enable qemu-guest-agent
  - systemctl start qemu-guest-agent"
```

### Clone VM with Cloud-Init

```bash
curl -X POST "http://localhost:8443/api2/json/nodes/pve/qemu/9000/clone" \
  -H "Authorization: <Proxmox Auth Header>" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "newid=101" \
  -d "name=cloned-vm" \
  -d "full=1" \
  -d "cloudinit_storage=nfs-storage" \
  -d "cloudinit_userdata=#cloud-config
hostname: cloned-vm
users:
  - name: admin
    sudo: ALL=(ALL) NOPASSWD:ALL
    ssh_authorized_keys:
      - ssh-rsa AAAA..."
```

### All Other API Calls

All other Proxmox API endpoints are passed through unchanged:

```bash
# List VMs
curl "http://localhost:8443/api2/json/nodes/pve/qemu" \
  -H "X-Api-Key: your-proxy-api-key" 

# Get VM status
curl "http://localhost:8443/api2/json/nodes/pve/qemu/100/status/current" \
  -H "X-Api-Key: your-proxy-api-key"
```

## How It Works

1. Client sends VM creation request with `cloudinit_userdata` and `cloudinit_storage`
2. Proxy extracts the cloud-init content
3. Proxy writes `vm-{vmid}-cloud-init-user.yaml` to `{storage}/snippets/`
4. Proxy adds `cicustom=user={storage}:snippets/vm-{vmid}-cloud-init-user.yaml` to the request
5. Proxy forwards modified request to Proxmox
6. If Proxmox returns an error, proxy deletes the snippet file (rollback)
7. Proxy returns Proxmox response to client

## Storage Setup

### NFS (Recommended for Clusters)

1. On Proxmox, add NFS storage with snippets content type enabled
2. Mount the same NFS share on the Docker host
3. Configure the storage mapping in the proxy

## License

MIT
