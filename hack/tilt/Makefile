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
KIND_VERSION ?= v0.30.0
TILT_BIN=${BIN_DIR}/tilt
KIND_BIN=${BIN_DIR}/kind

KIND_CLUSTER_NAME ?= volcano-dev
KIND_CONTEXT_NAME ?= ${KIND_CLUSTER_NAME}
KIND_CONFIG ?= hack/e2e-kind-config.yaml

TILT_WAIT_TIMEOUT ?= 10m
TILT_WAIT_RESOURCES ?= \
	uiresource/uncategorized \
	uiresource/volcano-namespaces \
	uiresource/volcano-crds \
	uiresource/volcano-vcjob-roles \
	uiresource/volcano-admission-init \
	uiresource/volcano-admission \
	uiresource/volcano-scheduler \
	uiresource/volcano-controllers \
	uiresource/prometheus-deployment \
	uiresource/kube-state-metrics \
	uiresource/grafana

# Install Kind if not present in _output/bin or wrong version
dev-install-kind:
	@PATH="${BIN_DIR}:$$PATH" KIND_BIN="${KIND_BIN}" KIND_VERSION="${KIND_VERSION}" bash -c 'source hack/lib/install.sh; check-kind'

# Create Kind cluster for local dev
dev-create-kind-cluster:
	@PATH="${BIN_DIR}:$$PATH" bash -c 'source hack/lib/install.sh; CLUSTER_CONTEXT=("--name" "${KIND_CLUSTER_NAME}"); KIND_OPT="--config ${KIND_CONFIG} --wait 5m"; kind-create-cluster'

.PHONY: dev-up
dev-up: dev-install-tilt dev-install-kind
	$(MAKE) dev-create-kind-cluster
	$(TILT_BIN) up -f hack/tilt/Tiltfile

.PHONY: dev-down
dev-down:
	@$(TILT_BIN) down -f hack/tilt/Tiltfile 2>/dev/null || true

# Delete Kind cluster
.PHONY: dev-delete-kind-cluster
dev-delete-kind-cluster:
	@${KIND_BIN} delete cluster --name ${KIND_CLUSTER_NAME} || true

.PHONY: dev-clean
dev-clean: dev-down dev-delete-kind-cluster
	rm -f $(KIND_BIN) $(TILT_BIN)

.PHONY: dev-status
dev-status:
	${KIND_BIN} get clusters
	@echo ---
	@kubectl cluster-info --context ${KIND_CONTEXT_NAME} 2>/dev/null || kubectl cluster-info --context kind-${KIND_CLUSTER_NAME} 2>/dev/null || echo "Cluster not running."

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

.PHONY: dev-tilt-status
dev-tilt-status: dev-install-tilt
	${TILT_BIN} get uiresource

.PHONY: dev-tilt-logs
dev-tilt-logs: dev-install-tilt
	@if [ -n "${RESOURCE}" ]; then \
		${TILT_BIN} logs ${RESOURCE}; \
	else \
		${TILT_BIN} logs; \
	fi

.PHONY: dev-tilt-describe
dev-tilt-describe: dev-install-tilt
	@if [ -z "${RESOURCE}" ]; then \
		echo "RESOURCE is required. Example: make dev-tilt-describe RESOURCE=volcano-scheduler"; \
		exit 1; \
	fi
	${TILT_BIN} describe uiresource ${RESOURCE}

.PHONY: dev-tilt-trigger
dev-tilt-trigger: dev-install-tilt
	@if [ -z "${RESOURCE}" ]; then \
		echo "RESOURCE is required. Example: make dev-tilt-trigger RESOURCE=volcano-scheduler"; \
		exit 1; \
	fi
	${TILT_BIN} trigger ${RESOURCE}

.PHONY: dev-wait-ready
dev-wait-ready: dev-install-tilt
	${TILT_BIN} wait --for=condition=Ready ${TILT_WAIT_RESOURCES} --timeout=${TILT_WAIT_TIMEOUT}
