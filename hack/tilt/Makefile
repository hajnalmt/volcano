# Copyright 2025 The Volcano Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This Makefile expects the following variables to be defined by the parent/root Makefile:
# OUTPUT_DIR, BIN_DIR
ifndef OUTPUT_DIR
$(error OUTPUT_DIR is not set. Please define it in the parent Makefile.)
endif

ifndef BIN_DIR
$(error BIN_DIR is not set. Please define it in the parent Makefile.)
endif

TILT_VERSION=0.35.1
K3D_VERSION=v5.8.3
TILT_BIN=${BIN_DIR}/tilt
K3D_BIN=${BIN_DIR}/k3d

K3D_DATA_DIR?=$(OUTPUT_DIR)/k3d-data
K3D_CLUSTER_NAME ?= volcano-dev
K3D_REGISTRY_NAME ?= volcano-registry
K3D_REGISTRY_PORT ?= 5000
K3D_SERVERS ?= 1
K3D_AGENTS ?= 3
K3D_API_PORT ?= 6510

# Install Tilt if not present in _output/bin or wrong version
dev-install-tilt:
	@if [ ! -f ${TILT_BIN} ] || [ "$$(${TILT_BIN} version | grep -Eo '[0-9]+\.[0-9]+\.[0-9]+' | head -1)" != "${TILT_VERSION}" ]; then \
		curl -fsSL https://github.com/tilt-dev/tilt/releases/download/v${TILT_VERSION}/tilt.${TILT_VERSION}.linux.x86_64.tar.gz | tar -xz && \
		mv tilt ${TILT_BIN}; \
		chmod +x ${TILT_BIN}; \
		printf "Tilt installed at ${TILT_BIN} (version ${TILT_VERSION}).\n"; \
	else \
		printf "Tilt already installed at ${TILT_BIN} (version ${TILT_VERSION}).\n"; \
	fi

# Install k3d if not present in _output/bin or wrong version
dev-install-k3d:
	@if [ ! -f ${K3D_BIN} ] || [ "$$(${K3D_BIN} version | grep -Eo 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)" != "${K3D_VERSION}" ]; then \
		curl -fsSL https://github.com/k3d-io/k3d/releases/download/${K3D_VERSION}/k3d-linux-amd64 -o ${K3D_BIN}; \
		chmod +x ${K3D_BIN}; \
		printf "k3d installed at ${K3D_BIN} (version ${K3D_VERSION}).\n"; \
	else \
		printf "k3d already installed at ${K3D_BIN} (version ${K3D_VERSION}).\n"; \
	fi

# Create k3d cluster for local dev
dev-create-k3d-cluster:
	mkdir -pv $(K3D_DATA_DIR)/k3s_storage
	mkdir -pv $(K3D_DATA_DIR)/machine-id-server-0
	mkdir -pv $(K3D_DATA_DIR)/containerd/server-0
	@for i in $$(seq 0 $$(($${K3D_AGENTS} - 1))); do \
		mkdir -pv $(K3D_DATA_DIR)/containerd/agent-$$i; \
		mkdir -pv $(K3D_DATA_DIR)/machine-id-agent-$$i; \
    done
	@( ${K3D_BIN} cluster list | grep -w $(K3D_CLUSTER_NAME) && echo "Cluster already exists." ) \
	|| ${K3D_BIN} cluster create ${K3D_CLUSTER_NAME} \
		--servers $(K3D_SERVERS) \
		--agents $(K3D_AGENTS) \
		--api-port $(K3D_API_PORT) \
		--registry-use k3d-$(K3D_REGISTRY_NAME):$(K3D_REGISTRY_PORT) \
		--no-lb \
		--volume $(K3D_DATA_DIR)/k3s_storage:/var/lib/rancher/k3s/storage \
		--volume $(K3D_DATA_DIR)/machine-id-server-0:/etc/machine-id@server:0 \
		--volume $(K3D_DATA_DIR)/containerd/server-0:/var/lib/rancher/k3s/agent/containerd@server:0 \
		$(foreach i,$(shell seq 0 $$(($(K3D_AGENTS)-1))),--volume $(K3D_DATA_DIR)/machine-id-agent-$(i):/etc/machine-id@agent:$(i) ) \
		$(foreach i,$(shell seq 0 $$(($(K3D_AGENTS)-1))),--volume $(K3D_DATA_DIR)/containerd/agent-$(i):/var/lib/rancher/k3s/agent/containerd@agent:$(i) ) \
		--wait 

.PHONY: dev-start-k3d-cluster
dev-start-k3d-cluster:
	@${K3D_BIN} cluster start ${K3D_CLUSTER_NAME}

.PHONY: dev-up
dev-up: dev-install-tilt dev-install-k3d dev-registry-create
	@cluster_status="$$($${K3D_BIN} cluster list | grep -w $${K3D_CLUSTER_NAME})"; \
	if [ -n "$$cluster_status" ]; then \
	    server_status=$$(echo "$$cluster_status" | awk '{print $$2}'); \
        if echo "$$server_status" | grep -q '^0/'; then \
            echo "Cluster exists but is stopped. Starting..."; \
            $(MAKE) dev-start-k3d-cluster; \
        else \
            echo "Cluster already running."; \
        fi; \
	else \
		$(MAKE) dev-create-k3d-cluster; \
	fi;
	$(TILT_BIN) up -f hack/tilt/Tiltfile

.PHONY: dev-down
dev-down: dev-stop-k3d-cluster

.PHONY: dev-clean
dev-clean: dev-delete-k3d-cluster dev-delete-registry
	rm -rf $(K3D_DATA_DIR)
	rm -f $(K3D_BIN) $(TILT_BIN)

.PHONY: dev-stop-k3d-cluster
dev-stop-k3d-cluster:
	@${K3D_BIN} cluster stop ${K3D_CLUSTER_NAME} || true

# Delete k3d cluster
.PHONY: dev-delete-k3d-cluster
dev-delete-k3d-cluster:
	@${K3D_BIN} cluster delete ${K3D_CLUSTER_NAME} || true

# Delete k3d cluster
.PHONY: dev-delete-registry
dev-delete-registry:
	@${K3D_BIN} registry delete ${K3D_REGISTRY_NAME} || true

# Stop local dev environment
.PHONY: dev-down
dev-down: dev-stop-k3d-cluster

.PHONY: dev-registry-create
dev-registry-create:
	@( ${K3D_BIN} registry ls k3d-${K3D_REGISTRY_NAME} && echo "Local registry already exists." ) \
	|| ${K3D_BIN} registry create ${K3D_REGISTRY_NAME} --port 0.0.0.0:${K3D_REGISTRY_PORT}

.PHONY: dev-registry-delete
dev-registry-delete:
	${K3D_BIN} registry delete ${K3D_REGISTRY_NAME}

.PHONY: dev-status
dev-status:
	${K3D_BIN} registry ls
	@echo ---
	${K3D_BIN} cluster ls


