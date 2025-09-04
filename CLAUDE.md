# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

The kustomize-controller is a Kubernetes controller that implements GitOps-style continuous delivery for Kubernetes manifests using Kustomize. It's part of the Flux ecosystem and reconciles `Kustomization` resources to apply and manage Kubernetes manifests from Git repositories and other sources.

## Essential Commands

### Testing
- `make test` - Run the full test suite (includes code generation, formatting, validation, and unit tests)
- `make install-envtest` - Install required test environment binaries
- `make fuzz-native` - Run native Go fuzz tests for the configured duration
- `make fuzz-smoketest` - Build and run fuzz tests once to verify they work

### Development
- `make manager` - Build the controller binary to `build/bin/manager`  
- `make run` - Run the controller locally against the configured Kubernetes cluster
- `make generate` - Generate Kubernetes code (deepcopy methods, etc.)
- `make manifests` - Generate CRD and RBAC manifests
- `make api-docs` - Generate API reference documentation
- `make tidy` - Clean up Go modules in both root and api directories

### Code Quality
- `make fmt` - Format Go code in both root and api directories
- `make vet` - Run Go vet static analysis in both root and api directories

### Kubernetes Development
- `make install` - Install CRDs into the configured Kubernetes cluster
- `make uninstall` - Remove CRDs from the cluster  
- `make deploy` - Deploy the controller to the cluster
- `make dev-deploy` - Deploy development version of the controller
- `make dev-cleanup` - Clean up development deployment

### Container Operations
- `make docker-build` - Build container image (set IMG environment variable)
- `make docker-push` - Push container image to registry
- `make docker-deploy` - Update running controller with new image

## Architecture Overview

### Core Components

**Main Controller**: `internal/controller/kustomization_controller.go`
- Implements the primary reconciliation logic for Kustomization resources
- Handles fetching artifacts from sources, running Kustomize builds, and applying manifests to clusters
- Manages dependency ordering, health checking, and drift detection

**API Types**: `api/v1/kustomization_types.go` 
- Defines the Kustomization Custom Resource Definition schema
- Contains the core data structures that define desired state for Kustomize operations
- Supports multiple API versions (v1, v1beta1, v1beta2) with conversion webhooks

**Key Internal Packages**:
- `internal/decryptor/` - SOPS decryption for sealed secrets (AWS KMS, Azure Key Vault, age)
- `internal/inventory/` - Tracks applied resources for garbage collection and drift detection  
- `internal/sops/` - SOPS key service providers (AWS KMS, Azure Key Vault)
- `internal/cache/` - Artifact and token caching systems

### Reconciliation Flow

1. **Source Watching**: Controller watches Source objects (GitRepository, Bucket, OCIRepository) for changes
2. **Artifact Fetching**: Downloads and extracts source artifacts when revisions change
3. **Kustomize Processing**: Runs kustomize build with transformers, patches, and variable substitution  
4. **Manifest Validation**: Validates generated manifests with Kubernetes API server dry-run
5. **Server-Side Apply**: Applies manifests using Kubernetes server-side apply for field management
6. **Health Assessment**: Monitors applied resources for readiness and health status
7. **Drift Detection**: Periodically checks for configuration drift and corrects it
8. **Pruning**: Removes resources no longer present in source manifests
9. **Status Reporting**: Updates Kustomization status with reconciliation results

### Dependencies and Integration

**Source Controller**: Requires source-controller for artifact serving
- Set `SOURCE_CONTROLLER_LOCALHOST=localhost:8080` when running source-controller locally
- Port forward with: `kubectl -n flux-system port-forward svc/source-controller 8080:80`

**Feature Gates**: Uses feature gates for experimental functionality (see `internal/features/`)
- Enable/disable with command line flags
- Check feature documentation before using experimental features

**Security Integration**: 
- Supports SOPS encryption with multiple key providers
- Service account impersonation for multi-tenancy
- RBAC integration for access control

## Development Patterns

### Testing Strategy
- Unit tests co-located with source files (`*_test.go`)
- Integration tests in `internal/controller/` using envtest
- Fuzz testing for security-critical parsing functions
- Testdata in `internal/controller/testdata/` for various scenarios

### Code Organization
- Controller logic separated into focused files by feature area
- API types versioned with conversion webhooks for backward compatibility
- Internal packages organized by functional domain (decryptor, inventory, etc.)
- Clear separation between controller logic and Kubernetes client operations

### Configuration Management
- Extensive use of feature gates for experimental functionality
- Command-line flag configuration with sensible defaults
- Environment variable support for runtime configuration
- Kubernetes-native configuration through Custom Resources

## Local Development Setup

1. Install dependencies: Go 1.25+, Docker, Kustomize, kubectl
2. Install CRDs: `make install`
3. Run source-controller locally or port-forward to existing instance
4. Set environment: `export SOURCE_CONTROLLER_LOCALHOST=localhost:8080`
5. Run controller: `make run`
6. Scale down in-cluster controller if needed: `kubectl -n flux-system scale deployment/kustomize-controller --replicas=0`

## Notes

- Uses Kubebuilder framework for controller scaffolding
- Implements Kubernetes controller-runtime patterns
- Integrates with Flux source types (Git, OCI, S3-compatible buckets)
- Supports multi-tenancy through namespace isolation and service account impersonation
- SOPS integration enables GitOps workflows with encrypted secrets