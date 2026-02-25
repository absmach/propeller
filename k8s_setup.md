# Single-node “real” Kubernetes cluster (kubeadm + containerd) on Linux

This tutorial sets up a **single machine** as both **control-plane + worker**, using **kubeadm** (the standard Kubernetes bootstrapping tool) and **containerd** (recommended runtime). It’s the closest thing to a “real” DIY cluster you’ll run on one box.

> Assumptions:
> - Ubuntu/Debian-like Linux (examples use `apt`)
> - You have `sudo` access
> - You want a single-node cluster that can run workloads (not just control plane)

---

## 0) Prep the machine

### Set a stable hostname (recommended)
```bash
sudo hostnamectl set-hostname k8s-single
```

### Disable swap (required by kubelet)
```bash
sudo swapoff -a
sudo sed -i.bak '/\sswap\s/s/^/#/' /etc/fstab
```

### Load kernel modules + sysctl for Kubernetes networking
```bash
cat <<'EOF' | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF

sudo modprobe overlay
sudo modprobe br_netfilter

cat <<'EOF' | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF

sudo sysctl --system
```

## 1) Install containerd (container runtime)

Kubernetes needs a container runtime; containerd is a common choice.

### Install + enable
```bash
sudo apt-get update
sudo apt-get install -y containerd
sudo systemctl enable --now containerd
```

### Configure containerd to use systemd cgroups (recommended)

Kubeadm clusters work best when the runtime cgroup driver matches kubelet (often systemd).

```bash
sudo mkdir -p /etc/containerd
containerd config default | sudo tee /etc/containerd/config.toml >/dev/null

# Set SystemdCgroup = true
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml

sudo systemctl restart containerd
```

## 2) Install kubeadm / kubelet / kubectl

Follow the official “Installing kubeadm” instructions for your distro/version.

On Ubuntu/Debian this typically means:

 - add the Kubernetes package repo for your target version

 - install:

    - kubelet (node agent)

    - kubeadm (cluster bootstrap)

    - kubectl (CLI)

After installing, verify:

```bash
kubeadm version
kubectl version --client
```

## 3) Bootstrap the single-node cluster (control plane)

Pick a Pod network CIDR that matches your CNI choice.

 - For Calico, a common choice is 192.168.0.0/16.

### Initialize

```bash
sudo kubeadm init --pod-network-cidr=192.168.0.0/16

kubeadm init creates the control plane node.
```

### Configure kubectl for your user

Run the exact commands printed at the end of kubeadm init, typically:

```bash
mkdir -p $HOME/.kube
sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
sudo chown $(id -u):$(id -g) $HOME/.kube/config
```

Check node status:

```bash
kubectl get nodes
```

## 4) Install a CNI (pod network) — example: Calico

Kubernetes won’t schedule pods normally until a CNI is installed.

### Install Calico (manifest method)

```bash
curl -LO https://raw.githubusercontent.com/projectcalico/calico/v3.31.3/manifests/calico.yaml
kubectl apply -f calico.yaml
```

Calico’s docs note that if you use the default ```192.168.0.0/16```, you usually don’t need to edit the manifest.

Wait for pods:

```bash
kubectl get pods -A --watch
```

## 5) Make it a true single-node cluster (schedule workloads on control plane)

By default, control-plane nodes repel normal workloads via a taint. You remove it for single-node setups.

```bash
kubectl taint nodes --all node-role.kubernetes.io/control-plane-
```

Confirm:

```bash
kubectl get nodes -o wide
```

If everything was completed, run a quick test.