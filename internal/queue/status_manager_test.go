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
	"testing"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
)

func TestNewQueueStatusManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	tests := []struct {
		name string
		opts QueueStatusManagerOptions
		want *QueueStatusManager
	}{
		{
			name: "default options",
			opts: QueueStatusManagerOptions{},
			want: &QueueStatusManager{
				client:            fakeClient,
				items:             make(map[string]QueueItem),
				enablePositions:   false,
				enableEstimation:  false,
				updateInterval:    30 * time.Second,
				avgProcessingTime: 2 * time.Minute,
			},
		},
		{
			name: "with position tracking",
			opts: QueueStatusManagerOptions{
				EnablePositions: true,
			},
			want: &QueueStatusManager{
				client:            fakeClient,
				items:             make(map[string]QueueItem),
				enablePositions:   true,
				enableEstimation:  false,
				updateInterval:    30 * time.Second,
				avgProcessingTime: 2 * time.Minute,
			},
		},
		{
			name: "with time estimation",
			opts: QueueStatusManagerOptions{
				EnableEstimation: true,
			},
			want: &QueueStatusManager{
				client:            fakeClient,
				items:             make(map[string]QueueItem),
				enablePositions:   false,
				enableEstimation:  true,
				updateInterval:    30 * time.Second,
				avgProcessingTime: 2 * time.Minute,
			},
		},
		{
			name: "custom update interval",
			opts: QueueStatusManagerOptions{
				UpdateInterval: 1 * time.Minute,
			},
			want: &QueueStatusManager{
				client:            fakeClient,
				items:             make(map[string]QueueItem),
				enablePositions:   false,
				enableEstimation:  false,
				updateInterval:    1 * time.Minute,
				avgProcessingTime: 2 * time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewQueueStatusManager(fakeClient, tt.opts)

			if got.enablePositions != tt.want.enablePositions {
				t.Errorf("NewQueueStatusManager() enablePositions = %v, want %v", got.enablePositions, tt.want.enablePositions)
			}
			if got.enableEstimation != tt.want.enableEstimation {
				t.Errorf("NewQueueStatusManager() enableEstimation = %v, want %v", got.enableEstimation, tt.want.enableEstimation)
			}
			if got.updateInterval != tt.want.updateInterval {
				t.Errorf("NewQueueStatusManager() updateInterval = %v, want %v", got.updateInterval, tt.want.updateInterval)
			}
		})
	}
}

func TestQueueStatusManager_TrackQueued(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	tests := []struct {
		name             string
		enablePositions  bool
		enableEstimation bool
		expectPositions  bool
		expectEstimation bool
	}{
		{
			name:             "basic queue tracking",
			enablePositions:  false,
			enableEstimation: false,
			expectPositions:  false,
			expectEstimation: false,
		},
		{
			name:             "with position tracking",
			enablePositions:  true,
			enableEstimation: false,
			expectPositions:  true,
			expectEstimation: false,
		},
		{
			name:             "with time estimation",
			enablePositions:  false,
			enableEstimation: true,
			expectPositions:  false,
			expectEstimation: true,
		},
		{
			name:             "with both position and estimation",
			enablePositions:  true,
			enableEstimation: true,
			expectPositions:  true,
			expectEstimation: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh kustomization for each test to avoid shared state
			kustomization := &kustomizev1.Kustomization{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kustomizev1.GroupVersion.String(),
					Kind:       "Kustomization",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-kustomization",
					Namespace: "test-namespace",
				},
				Status: kustomizev1.KustomizationStatus{
					ObservedGeneration: -1, // Undiscovered state
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(kustomization).
				WithStatusSubresource(&kustomizev1.Kustomization{}).
				Build()

			mgr := NewQueueStatusManager(fakeClient, QueueStatusManagerOptions{
				EnablePositions:  tt.enablePositions,
				EnableEstimation: tt.enableEstimation,
			})

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "test-namespace",
					Name:      "test-kustomization",
				},
			}

			ctx := context.TODO()
			err := mgr.TrackQueued(ctx, req)
			if err != nil {
				t.Fatalf("TrackQueued() error = %v", err)
			}

			// Verify the item was tracked
			if mgr.GetQueueDepth() != 1 {
				t.Errorf("GetQueueDepth() = %d, want 1", mgr.GetQueueDepth())
			}

			// Verify the resource status was updated
			var updatedKustomization kustomizev1.Kustomization
			err = fakeClient.Get(ctx, req.NamespacedName, &updatedKustomization)
			if err != nil {
				t.Fatalf("Failed to get updated kustomization: %v", err)
			}

			// Should be set to "discovered but not processed" state
			if updatedKustomization.Status.ObservedGeneration != 0 {
				t.Errorf("ObservedGeneration = %d, want 0", updatedKustomization.Status.ObservedGeneration)
			}

			// Should have queue metadata
			if updatedKustomization.Status.QueueMetadata == nil {
				t.Error("QueueMetadata should not be nil")
			} else {
				if updatedKustomization.Status.QueueMetadata.QueuedAt == nil {
					t.Error("QueuedAt should not be nil")
				}

				if tt.expectPositions {
					if updatedKustomization.Status.QueueMetadata.Position == nil {
						t.Error("Position should not be nil when position tracking is enabled")
					} else if *updatedKustomization.Status.QueueMetadata.Position != 1 {
						t.Errorf("Position = %d, want 1", *updatedKustomization.Status.QueueMetadata.Position)
					}
				} else {
					if updatedKustomization.Status.QueueMetadata.Position != nil {
						t.Error("Position should be nil when position tracking is disabled")
					}
				}

				if tt.expectEstimation {
					if updatedKustomization.Status.QueueMetadata.EstimatedProcessingTime == nil {
						t.Error("EstimatedProcessingTime should not be nil when time estimation is enabled")
					}
				} else {
					if updatedKustomization.Status.QueueMetadata.EstimatedProcessingTime != nil {
						t.Error("EstimatedProcessingTime should be nil when time estimation is disabled")
					}
				}
			}

			// Should have appropriate conditions
			readyCondition := conditions.Get(&updatedKustomization, meta.ReadyCondition)
			if readyCondition == nil {
				t.Error("Ready condition should exist")
			} else {
				if readyCondition.Status != metav1.ConditionFalse {
					t.Errorf("Ready condition status = %s, want %s", readyCondition.Status, metav1.ConditionFalse)
				}
				if readyCondition.Reason != "Queued" {
					t.Errorf("Ready condition reason = %s, want Queued", readyCondition.Reason)
				}
			}

			reconcilingCondition := conditions.Get(&updatedKustomization, meta.ReconcilingCondition)
			if reconcilingCondition == nil {
				t.Error("Reconciling condition should exist")
			} else {
				if reconcilingCondition.Status != metav1.ConditionFalse {
					t.Errorf("Reconciling condition status = %s, want %s", reconcilingCondition.Status, metav1.ConditionFalse)
				}
				if reconcilingCondition.Reason != "Pending" {
					t.Errorf("Reconciling condition reason = %s, want Pending", reconcilingCondition.Reason)
				}
			}
		})
	}
}

func TestQueueStatusManager_TrackDequeued(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewQueueStatusManager(fakeClient, QueueStatusManagerOptions{})

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-kustomization",
		},
	}

	// Add an item to track
	mgr.items[req.String()] = QueueItem{
		Request:  req,
		QueuedAt: time.Now(),
		Position: 1,
	}

	// Verify it's tracked
	if mgr.GetQueueDepth() != 1 {
		t.Errorf("GetQueueDepth() = %d, want 1", mgr.GetQueueDepth())
	}

	// Track dequeued
	mgr.TrackDequeued(req)

	// Verify it's no longer tracked
	if mgr.GetQueueDepth() != 0 {
		t.Errorf("GetQueueDepth() = %d, want 0", mgr.GetQueueDepth())
	}
}

func TestQueueStatusManager_TrackQueuedAlreadyProcessed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)

	// Create a test kustomization with observedGeneration: 1 (already processed)
	kustomization := &kustomizev1.Kustomization{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kustomizev1.GroupVersion.String(),
			Kind:       "Kustomization",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kustomization",
			Namespace: "test-namespace",
		},
		Status: kustomizev1.KustomizationStatus{
			ObservedGeneration: 1, // Already processed
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(kustomization).
		WithStatusSubresource(&kustomizev1.Kustomization{}).
		Build()

	mgr := NewQueueStatusManager(fakeClient, QueueStatusManagerOptions{})

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-kustomization",
		},
	}

	ctx := context.TODO()
	err := mgr.TrackQueued(ctx, req)
	if err != nil {
		t.Fatalf("TrackQueued() error = %v", err)
	}

	// Verify the resource status was NOT updated (since it's already processed)
	var updatedKustomization kustomizev1.Kustomization
	err = fakeClient.Get(ctx, req.NamespacedName, &updatedKustomization)
	if err != nil {
		t.Fatalf("Failed to get updated kustomization: %v", err)
	}

	// Should still be 1 (unchanged)
	if updatedKustomization.Status.ObservedGeneration != 1 {
		t.Errorf("ObservedGeneration = %d, want 1 (unchanged)", updatedKustomization.Status.ObservedGeneration)
	}

	// Should not have queue metadata (since it wasn't updated)
	if updatedKustomization.Status.QueueMetadata != nil {
		t.Error("QueueMetadata should be nil for already-processed resource")
	}
}

func TestQueueStatusManager_GetQueuedItems(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	mgr := NewQueueStatusManager(fakeClient, QueueStatusManagerOptions{})

	// Add some test items
	req1 := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "ns1", Name: "name1"}}
	req2 := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "ns2", Name: "name2"}}

	mgr.items[req1.String()] = QueueItem{Request: req1, QueuedAt: time.Now(), Position: 1}
	mgr.items[req2.String()] = QueueItem{Request: req2, QueuedAt: time.Now(), Position: 2}

	items := mgr.GetQueuedItems()

	if len(items) != 2 {
		t.Errorf("GetQueuedItems() returned %d items, want 2", len(items))
	}

	// Verify we get copies, not references
	items[0].Position = 999
	if mgr.items[req1.String()].Position == 999 {
		t.Error("GetQueuedItems() should return copies, not references")
	}
}

func TestQueueStatusManager_FormatQueueMessage(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = kustomizev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	tests := []struct {
		name             string
		enablePositions  bool
		enableEstimation bool
		item             QueueItem
		wantContains     []string
		wantNotContains  []string
	}{
		{
			name:             "basic message",
			enablePositions:  false,
			enableEstimation: false,
			item: QueueItem{
				QueuedAt: time.Now().Add(-30 * time.Second),
			},
			wantContains:    []string{"Resource queued for reconciliation", "queued for"},
			wantNotContains: []string{"position", "estimated processing"},
		},
		{
			name:             "with position",
			enablePositions:  true,
			enableEstimation: false,
			item: QueueItem{
				QueuedAt: time.Now().Add(-30 * time.Second),
				Position: 5,
			},
			wantContains:    []string{"Resource queued for reconciliation", "position 5"},
			wantNotContains: []string{"estimated processing"},
		},
		{
			name:             "with estimation",
			enablePositions:  false,
			enableEstimation: true,
			item: QueueItem{
				QueuedAt:       time.Now().Add(-30 * time.Second),
				EstimatedStart: func() *time.Time { t := time.Now().Add(2 * time.Minute); return &t }(),
			},
			wantContains:    []string{"Resource queued for reconciliation", "estimated processing in"},
			wantNotContains: []string{"position"},
		},
		{
			name:             "processing soon",
			enablePositions:  false,
			enableEstimation: true,
			item: QueueItem{
				QueuedAt:       time.Now().Add(-30 * time.Second),
				EstimatedStart: func() *time.Time { t := time.Now().Add(-1 * time.Second); return &t }(),
			},
			wantContains:    []string{"Resource queued for reconciliation", "processing soon"},
			wantNotContains: []string{"position", "estimated processing in"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewQueueStatusManager(fakeClient, QueueStatusManagerOptions{
				EnablePositions:  tt.enablePositions,
				EnableEstimation: tt.enableEstimation,
			})

			msg := mgr.formatQueueMessage(tt.item)

			for _, want := range tt.wantContains {
				if !contains(msg, want) {
					t.Errorf("formatQueueMessage() = %q, want to contain %q", msg, want)
				}
			}

			for _, notWant := range tt.wantNotContains {
				if contains(msg, notWant) {
					t.Errorf("formatQueueMessage() = %q, should not contain %q", msg, notWant)
				}
			}
		})
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			func() bool {
				for i := 1; i <= len(s)-len(substr); i++ {
					if s[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}())))
}
