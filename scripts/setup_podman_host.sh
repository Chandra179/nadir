#!/usr/bin/env bash
# One-time Podman host setup for the dev laptop (Ubuntu 24.04+).
#
# Installs the podman engine and its rootless dependencies, the newest
# podman-compose provider (apt's 1.0.6 is too old for service_healthy
# depends_on and deploy.limits), the NVIDIA CDI toolchain when an NVIDIA GPU
# is present, and removes a stale docker group membership.
#
# Every step is idempotent; run again freely after OS upgrades.
#
# Usage:
#   ./scripts/setup_podman_host.sh            # engine + compose + GPU CDI
#   ./scripts/setup_podman_host.sh --skip-gpu # skip the NVIDIA toolkit
set -euo pipefail

SKIP_GPU=0
[ "${1:-}" = "--skip-gpu" ] && SKIP_GPU=1

step() { echo "==> $*"; }

step "Installing podman engine and rootless dependencies"
if ! command -v podman >/dev/null 2>&1; then
    sudo apt-get update -qq
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y \
        podman uidmap slirp4netns fuse-overlayfs catatonit
else
    echo "    podman already installed: $(podman --version)"
fi

step "Ensuring rootless subordinate ID ranges for ${USER}"
if ! grep -q "^${USER}:" /etc/subuid; then
    sudo usermod --add-subuids 100000-165535 --add-subgids 100000-165535 "$USER"
else
    echo "    ${USER} already has subordinate IDs"
fi

step "Configuring unqualified-search registries for the rootless user"
REGISTRIES_CONF="$HOME/.config/containers/registries.conf"
if ! grep -qs 'unqualified-search-registries' /etc/containers/registries.conf /etc/containers/registries.conf.d/*.conf 2>/dev/null; then
    mkdir -p "$HOME/.config/containers"
    if [ ! -f "$REGISTRIES_CONF" ]; then
        printf 'unqualified-search-registries = ["docker.io"]\n' > "$REGISTRIES_CONF"
        echo "    wrote $REGISTRIES_CONF"
    fi
else
    echo "    already configured system-wide"
fi

step "Installing the newest podman-compose via pipx"
if ! command -v pipx >/dev/null 2>&1; then
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y pipx
fi
if ! pipx list 2>/dev/null | grep -q podman-compose; then
    pipx install podman-compose
else
    pipx upgrade podman-compose || true
fi
case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) echo "    WARNING: ~/.local/bin is not on PATH; 'podman compose' will not find the provider."
       echo "             Add:  export PATH=\"\$HOME/.local/bin:\$PATH\"" ;;
esac

if [ "$SKIP_GPU" -eq 0 ] && command -v nvidia-smi >/dev/null 2>&1; then
    step "NVIDIA GPU detected: installing container toolkit and generating the CDI spec"
    if ! command -v nvidia-ctk >/dev/null 2>&1; then
        if ! apt-cache policy nvidia-container-toolkit 2>/dev/null | grep -q Candidate:; then
            echo "    nvidia-container-toolkit not in apt; adding NVIDIA's libnvidia-container repo"
            curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey \
                | sudo gpg --dearmor --yes -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
            curl -fsSL https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list \
                | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' \
                | sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list >/dev/null
            sudo apt-get update -qq
        fi
        sudo DEBIAN_FRONTEND=noninteractive apt-get install -y nvidia-container-toolkit
    fi
    if [ ! -f /etc/cdi/nvidia.yaml ]; then
        sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml
    fi
    # Podman 4.9 (Ubuntu 24.04) vendors the CDI 0.6.0 library and rejects the
    # 0.7.0 `additionalGids` field nvidia-ctk 1.20 emits — strip it and pin
    # the spec version so podman can load the spec. Harmless on newer podman.
    sudo python3 - <<'EOF'
import yaml

path = "/etc/cdi/nvidia.yaml"
spec = yaml.safe_load(open(path))
if spec.get("cdiVersion") != "0.6.0":
    for edits in [spec.get("containerEdits")] + [d.get("containerEdits") for d in spec.get("devices", [])]:
        if isinstance(edits, dict):
            edits.pop("additionalGids", None)
    spec["cdiVersion"] = "0.6.0"
    open(path, "w").write(yaml.safe_dump(spec, sort_keys=False))
    print(f"    downgraded {path} to CDI 0.6.0 for podman 4.9")
EOF
    echo "    CDI spec ready: /etc/cdi/nvidia.yaml"
elif [ "$SKIP_GPU" -eq 1 ]; then
    echo "==> Skipping GPU setup (--skip-gpu)"
else
    echo "==> No NVIDIA GPU detected; skipping GPU setup"
fi

step "Removing stale docker group membership (docker was never installed)"
if getent group docker >/dev/null 2>&1 && ! command -v docker >/dev/null 2>&1; then
    if id -nG "$USER" | tr ' ' '\n' | grep -qx docker; then
        sudo gpasswd -d "$USER" docker && echo "    removed $USER from the docker group"
    else
        echo "    $USER is not in the docker group"
    fi
else
    echo "    nothing to clean up"
fi

step "Final checks"
podman info >/dev/null
echo "    $(podman --version), compose provider: $(podman info --format '{{.Host.ComposeProviders}}' 2>/dev/null || echo 'run: podman compose version')"
echo "Done. Verify the GPU path with:"
echo "    podman run --rm --device nvidia.com/gpu=all alpine true   # needs the CDI spec"
