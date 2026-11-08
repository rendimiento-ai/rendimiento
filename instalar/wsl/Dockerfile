# The WSL distribution of rendimiento for Windows: Debian with systemd,
# containerd, BuildKit and the rendimiento program, all started by systemd
# when the distribution starts. It is not an image to run: each release
# exports its file system as rendimiento-wsl-<arch>.tar.gz (binarios.yml),
# which the Windows app (or a person) imports:
#
#   wsl --import rendimiento C:\rendimiento rendimiento-wsl-amd64.tar.gz
#
# The rendimiento program comes from the build context (rendimiento-linux-<arch>).
FROM debian:bookworm-slim

ARG TARGETARCH
ARG NERDCTL_VERSION=2.4.2

# systemd starts everything; iptables (legacy: what WSL2's kernel takes)
# for the containers' networks; curl for the checks.
RUN apt-get update \
 && apt-get install -y --no-install-recommends systemd systemd-sysv dbus ca-certificates curl iptables iproute2 \
 && update-alternatives --set iptables /usr/sbin/iptables-legacy \
 && update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy \
 && rm -rf /var/lib/apt/lists/*

# containerd, nerdctl, BuildKit and the CNI plugins, system-wide (the
# distribution is rendimiento's alone, so no rootless setup is needed).
# What rendimiento does not use (lazy pulling, encrypted images, the
# rootless tools, buildg…) goes, in the same step: half of the download.
RUN curl -fsSL "https://github.com/containerd/nerdctl/releases/download/v${NERDCTL_VERSION}/nerdctl-full-${NERDCTL_VERSION}-linux-${TARGETARCH}.tar.gz" \
    | tar -xz -C /usr/local \
 && cd /usr/local/bin \
 && rm -f containerd-stargz-grpc stargz-fuse-manager stargz-store stargz-store-helper ctr-enc ctd-decoder ctr-remote buildg \
       rootlesskit rootlessctl containerd-rootless.sh containerd-rootless-setuptool.sh bypass4netns bypass4netnsd \
       containerd-fuse-overlayfs-grpc fuse-overlayfs gomodjail nerdctl.gomodjail slirp4netns ipfs tini \
 && cd /usr/local/libexec/cni && rm -f dhcp tap vlan macvlan ipvlan sbr dummy vrf host-device bandwidth static \
 && rm -rf /usr/local/share/doc \
 && cp /usr/local/lib/systemd/system/containerd.service /usr/local/lib/systemd/system/buildkit.service /etc/systemd/system/ \
 && systemctl enable containerd.service buildkit.service

COPY rendimiento-linux-${TARGETARCH} /usr/local/bin/rendimiento
COPY wsl.conf /etc/wsl.conf
COPY rendimiento.service /etc/systemd/system/rendimiento.service
RUN chmod 0755 /usr/local/bin/rendimiento \
 && mkdir -p /etc/rendimiento \
 && systemctl enable rendimiento.service
