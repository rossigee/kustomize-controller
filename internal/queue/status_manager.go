/*
Copyright 2025 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
)

// QueueStatusManager tracks resources in the reconciliation queue and provides
// status visibility to improve user experience during high load conditions.
type QueueStatusManager struct {
	client client.Client
	mu     sync.RWMutex
	items  map[string]QueueItem

	// Configuration flags
	enablePositions  bool
	enableEstimation bool
	updateInterval   time.Duration

	// Metrics for estimation
	avgProcessingTime time.Duration
	queueDepthHistory []int
}

// QueueItem represents a resource in the reconciliation queue with metadata.
type QueueItem struct {
	Request        reconcile.Request
	QueuedAt       time.Time
	Position       int        // Optional: current position in queue
	EstimatedStart *time.Time // Optional: estimated processing time
}

// QueueStatusManagerOptions configures the behavior of QueueStatusManager.
type QueueStatusManagerOptions struct {
	// EnablePositions controls whether queue position tracking is enabled
	EnablePositions bool

	// EnableEstimation controls whether processing time estimation is enabled
	EnableEstimation bool

	// UpdateInterval controls how often queue positions are recalculated
	UpdateInterval time.Duration
}

// NewQueueStatusManager creates a new queue status manager with the given options.
func NewQueueStatusManager(client client.Client, opts QueueStatusManagerOptions) *QueueStatusManager {
	if opts.UpdateInterval == 0 {
		opts.UpdateInterval = 30 * time.Second
	}

	return &QueueStatusManager{
		client:            client,
		items:             make(map[string]QueueItem),
		enablePositions:   opts.EnablePositions,
		enableEstimation:  opts.EnableEstimation,
		updateInterval:    opts.UpdateInterval,
		avgProcessingTime: 2 * time.Minute, // Default estimate
	}
}

// TrackQueued records when a resource is queued for reconciliation and immediately
// updates its status to provide visibility into the queue state.
func (m *QueueStatusManager) TrackQueued(ctx context.Context, req reconcile.Request) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := req.String()
	item := QueueItem{
		Request:  req,
		QueuedAt: time.Now(),
	}

	// Calculate position if enabled
	position := len(m.items) + 1 // Current queue position
	if m.enablePositions {
		item.Position = position
	}

	// Calculate estimated processing time if enabled
	if m.enableEstimation {
		estimatedWait := time.Duration(position) * m.avgProcessingTime
		estimatedStart := time.Now().Add(estimatedWait)
		item.EstimatedStart = &estimatedStart
	}

	m.items[key] = item

	// Update resource status immediately
	return m.updateResourceStatus(ctx, req, item)
}

// TrackDequeued removes tracking when a resource starts processing.
// This should be called when the reconcile method begins processing a request.
func (m *QueueStatusManager) TrackDequeued(req reconcile.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := req.String()
	delete(m.items, key)
}

// updateResourceStatus sets the appropriate status fields to indicate the resource
// is queued for processing, replacing the misleading observedGeneration: -1 pattern.
func (m *QueueStatusManager) updateResourceStatus(ctx context.Context, req reconcile.Request, item QueueItem) error {
	obj := &kustomizev1.Kustomization{}
	if err := m.client.Get(ctx, req.NamespacedName, obj); err != nil {
		// Resource might have been deleted, which is fine
		return client.IgnoreNotFound(err)
	}

	// Only update if resource is still in "undiscovered" state
	if obj.Status.ObservedGeneration != -1 {
		return nil // Already processed or processing
	}

	// Create a copy for modification
	patch := obj.DeepCopy()

	// Set to "discovered but not processed" state
	patch.Status.ObservedGeneration = 0

	// Add queue metadata
	patch.Status.QueueMetadata = &kustomizev1.QueueMetadata{
		QueuedAt: &metav1.Time{Time: item.QueuedAt},
	}

	if m.enablePositions && item.Position > 0 {
		patch.Status.QueueMetadata.Position = &item.Position
	}

	if m.enableEstimation && item.EstimatedStart != nil {
		patch.Status.QueueMetadata.EstimatedProcessingTime = &metav1.Time{Time: *item.EstimatedStart}
	}

	// Set appropriate conditions
	message := m.formatQueueMessage(item)
	conditions.MarkFalse(patch, meta.ReadyCondition, "Queued", "%s", message)
	conditions.MarkFalse(patch, meta.ReconcilingCondition, "Pending", "Waiting for controller capacity")

	// Update status
	return m.client.Status().Update(ctx, patch)
}

// formatQueueMessage creates a user-friendly message about queue status.
func (m *QueueStatusManager) formatQueueMessage(item QueueItem) string {
	baseMsg := "Resource queued for reconciliation"

	var details []string

	if m.enablePositions && item.Position > 0 {
		details = append(details, fmt.Sprintf("position %d", item.Position))
	}

	if m.enableEstimation && item.EstimatedStart != nil {
		waitTime := time.Until(*item.EstimatedStart)
		if waitTime > 0 {
			details = append(details, fmt.Sprintf("estimated processing in %v", waitTime.Round(time.Second)))
		} else {
			details = append(details, "processing soon")
		}
	} else {
		// Fallback: show how long it's been queued
		queuedDuration := time.Since(item.QueuedAt)
		details = append(details, fmt.Sprintf("queued for %v", queuedDuration.Round(time.Second)))
	}

	if len(details) > 0 {
		return fmt.Sprintf("%s (%s)", baseMsg, fmt.Sprintf("%s", details[0]))
	}

	return baseMsg
}

// StartStatusUpdater runs a background process to periodically update queue positions
// and estimated processing times for all tracked resources.
func (m *QueueStatusManager) StartStatusUpdater(ctx context.Context) {
	if !m.enablePositions && !m.enableEstimation {
		return // No periodic updates needed
	}

	ticker := time.NewTicker(m.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.updateAllPositions(ctx)
		}
	}
}

// updateAllPositions recalculates positions and estimated processing times
// for all tracked resources.
func (m *QueueStatusManager) updateAllPositions(ctx context.Context) {
	m.mu.Lock()
	items := make([]QueueItem, 0, len(m.items))
	for _, item := range m.items {
		items = append(items, item)
	}
	m.mu.Unlock()

	// Skip if no items to update
	if len(items) == 0 {
		return
	}

	// Update positions and estimates
	for i, item := range items {
		updated := false

		if m.enablePositions {
			newPosition := i + 1
			if item.Position != newPosition {
				item.Position = newPosition
				updated = true
			}
		}

		if m.enableEstimation {
			estimatedWait := time.Duration(item.Position) * m.avgProcessingTime
			newEstimatedStart := time.Now().Add(estimatedWait)
			if item.EstimatedStart == nil || !item.EstimatedStart.Equal(newEstimatedStart) {
				item.EstimatedStart = &newEstimatedStart
				updated = true
			}
		}

		if updated {
			m.mu.Lock()
			m.items[item.Request.String()] = item
			m.mu.Unlock()

			// Update the resource status
			if err := m.updateResourceStatus(ctx, item.Request, item); err != nil {
				// Log error but continue with other updates
				// In a real implementation, this would use proper logging
				continue
			}
		}
	}
}

// GetQueueDepth returns the current number of items in the tracked queue.
func (m *QueueStatusManager) GetQueueDepth() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items)
}

// GetQueuedItems returns a snapshot of currently queued items.
func (m *QueueStatusManager) GetQueuedItems() []QueueItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]QueueItem, 0, len(m.items))
	for _, item := range m.items {
		items = append(items, item)
	}
	return items
}
