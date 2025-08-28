# kwok node config
export KWOK_NODE_CPU=${KWOK_NODE_CPU:-8}      # 8 cores
export KWOK_NODE_MEMORY=${KWOK_NODE_MEMORY:-8Gi}  # 8GB

# install kwok nodes
function install-kwok-a100-nodes() {
  local node_count=$1
  for i in $(seq 0 $((node_count-1))); do
    create-kwok-a100-node $i
  done
}

# create kwok node
function create-kwok-a100-node() {
  local node_index=$1
  
  kubectl apply -f - <<EOF
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: fake
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: kwok-node-${node_index}
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: kwok
  name: kwok-node-${node_index}
spec:
  taints:
  - effect: NoSchedule
    key: kwok.x-k8s.io/node
    value: fake
status:
  capacity:
    cpu: "${KWOK_NODE_CPU}"
    memory: "${KWOK_NODE_MEMORY}"
    pods: "110"
    nvidia.com/A100: "2"
  allocatable:
    cpu: "${KWOK_NODE_CPU}"
    memory: "${KWOK_NODE_MEMORY}"
    pods: "110"
    nvidia.com/A100: "2"
EOF
}

# install kwok nodes
function install-kwok-h100-nodes() {
  local node_count=$1
  for i in $(seq 0 $((node_count-1))); do
    create-kwok-a100-node $i
  done
}

# create kwok node
function create-kwok-h100-node() {
  local node_index=$1
  
  kubectl apply -f - <<EOF
apiVersion: v1
kind: Node
metadata:
  annotations:
    node.alpha.kubernetes.io/ttl: "0"
    kwok.x-k8s.io/node: fake
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: kwok-node-${node_index}
    kubernetes.io/os: linux
    kubernetes.io/role: agent
    node-role.kubernetes.io/agent: ""
    type: kwok
  name: kwok-node-${node_index}
spec:
  taints:
  - effect: NoSchedule
    key: kwok.x-k8s.io/node
    value: fake
status:
  capacity:
    cpu: "${KWOK_NODE_CPU}"
    memory: "${KWOK_NODE_MEMORY}"
    pods: "110"
    nvidia.com/H100: "8"
  allocatable:
    cpu: "${KWOK_NODE_CPU}"
    memory: "${KWOK_NODE_MEMORY}"
    pods: "110"
    nvidia.com/H100: "8"
EOF
}
