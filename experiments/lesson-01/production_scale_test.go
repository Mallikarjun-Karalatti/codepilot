package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateRealisticRepository creates a multi-package, multi-service Go repository
// with 50 source files and over 150 functions, methods, and structs.
func generateRealisticRepository(t testing.TB, rootDir string) {
	packages := []struct {
		pkgName string
		files   int
	}{
		{"auth", 6},
		{"users", 8},
		{"billing", 8},
		{"orders", 8},
		{"inventory", 6},
		{"notifications", 6},
		{"storage", 4},
		{"middleware", 4},
	}

	for _, p := range packages {
		pkgDir := filepath.Join(rootDir, p.pkgName)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", pkgDir, err)
		}

		for f := 1; f <= p.files; f++ {
			filename := filepath.Join(pkgDir, fmt.Sprintf("service_%02d.go", f))
			content := fmt.Sprintf(`package %s

type Handler%s%02d struct {
	ID int
}

func (h *Handler%s%02d) Execute%02d(param string) bool {
	return len(param) > 0
}

func (h *Handler%s%02d) Validate%02d() bool {
	return true
}

func Helper%s%02d() string {
	return "%s_%02d"
}
`, p.pkgName, p.pkgName, f, p.pkgName, f, f, p.pkgName, f, f, p.pkgName, f, p.pkgName, f)

			if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
				t.Fatalf("failed to write %s: %v", filename, err)
			}
		}
	}

	// Add package relationship files in orders
	ordersDir := filepath.Join(rootDir, "orders")

	billingService := filepath.Join(ordersDir, "gateway.go")
	billingContent := `package orders

type PaymentGateway struct{}

func (g *PaymentGateway) ChargeCard(amount int) bool {
	return amount > 0
}

func (g *PaymentGateway) RefundCard(amount int) bool {
	return amount > 0
}
`
	if err := os.WriteFile(billingService, []byte(billingContent), 0o600); err != nil {
		t.Fatalf("write gateway error: %v", err)
	}

	orderService := filepath.Join(ordersDir, "checkout.go")
	orderContent := `package orders

type CheckoutService struct {
	Gateway *PaymentGateway
}

func (c *CheckoutService) ProcessOrder(total int) bool {
	return c.Gateway.ChargeCard(total)
}
`
	if err := os.WriteFile(orderService, []byte(orderContent), 0o600); err != nil {
		t.Fatalf("write orders checkout error: %v", err)
	}
}

func TestProductionScaleIncrementalLifecycle(t *testing.T) {
	ctx := context.Background()
	rootDir := t.TempDir()
	generateRealisticRepository(t, rootDir)

	storeDir := t.TempDir()
	store, err := NewFilesystemIndexStore(storeDir)
	if err != nil {
		t.Fatalf("NewFilesystemIndexStore error: %v", err)
	}

	embedder := &testEmbedderWithTracking{}
	config := DefaultIndexConfig()
	config.ParseWorkers = 8
	config.EmbedWorkers = 8

	indexer := &IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Store:    store,
	}

	// 1. Initial Indexing
	start := time.Now()
	indexed1, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("initial indexing failed: %v", err)
	}
	initialDuration := time.Since(start)

	if indexed1.Stats.FilesScanned < 50 {
		t.Errorf("FilesScanned = %d, expected >= 50", indexed1.Stats.FilesScanned)
	}
	if indexed1.Stats.ChunksEmbedded < 150 {
		t.Errorf("ChunksEmbedded = %d, expected >= 150", indexed1.Stats.ChunksEmbedded)
	}
	t.Logf("[Scale Test - Initial Index] Files: %d, Chunks: %d, Time: %v",
		indexed1.Stats.FilesScanned, indexed1.Stats.ChunksEmbedded, initialDuration)

	// 2. Immediate Re-index (Zero-Work Fast Path)
	embedder.reset()
	start = time.Now()
	indexed2, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("zero-work indexing failed: %v", err)
	}
	zeroWorkDuration := time.Since(start)

	if indexed2.Stats.ChunksEmbedded != 0 {
		t.Errorf("expected 0 chunks embedded on unchanged run, got %d", indexed2.Stats.ChunksEmbedded)
	}
	if indexed2.Stats.ChunksKept != indexed1.Stats.ChunksEmbedded {
		t.Errorf("expected %d chunks kept, got %d", indexed1.Stats.ChunksEmbedded, indexed2.Stats.ChunksKept)
	}
	t.Logf("[Scale Test - Zero-Work Run] Chunks Kept: %d, Embeddings Avoided: %d (100%%), Time: %v",
		indexed2.Stats.ChunksKept, indexed2.Stats.ChunksKept, zeroWorkDuration)

	// 3. Mutate 1 file out of 50
	embedder.reset()
	mutatedFile := filepath.Join(rootDir, "auth", "service_01.go")
	newAuthContent := `package auth

type Handlerauth01 struct {
	ID int
}

func (h *Handlerauth01) Execute01(param string) bool {
	return false
}

func (h *Handlerauth01) BrandNewMethod() string {
	return "brand_new"
}

func Helperauth01() string {
	return "modified"
}
`
	if err := os.WriteFile(mutatedFile, []byte(newAuthContent), 0o600); err != nil {
		t.Fatalf("write mutated file error: %v", err)
	}

	start = time.Now()
	indexed3, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("1-file mutation indexing failed: %v", err)
	}
	mutateDuration := time.Since(start)

	if indexed3.Stats.FilesModified != 1 {
		t.Errorf("FilesModified = %d, want 1", indexed3.Stats.FilesModified)
	}
	if indexed3.Stats.FilesUnchanged != indexed1.Stats.FilesScanned-1 {
		t.Errorf("FilesUnchanged = %d, want %d", indexed3.Stats.FilesUnchanged, indexed1.Stats.FilesScanned-1)
	}
	if indexed3.Stats.ChunksEmbedded != 4 { // struct + 2 methods + helper = 4
		t.Errorf("ChunksEmbedded = %d, want 4", indexed3.Stats.ChunksEmbedded)
	}
	t.Logf("[Scale Test - 1 File Mutated] Files Modified: %d, Chunks Embedded: %d, Work Avoided: %.1f%%, Time: %v",
		indexed3.Stats.FilesModified, indexed3.Stats.ChunksEmbedded,
		float64(indexed3.Stats.ChunksKept)/float64(len(indexed3.Chunks))*100, mutateDuration)

	// 4. Add a new file
	embedder.reset()
	addedFile := filepath.Join(rootDir, "storage", "cache.go")
	cacheContent := `package storage

type MemoryCache struct{}

func (c *MemoryCache) Get(k string) string { return "" }
func (c *MemoryCache) Set(k, v string) {}
`
	if err := os.WriteFile(addedFile, []byte(cacheContent), 0o600); err != nil {
		t.Fatalf("write added file error: %v", err)
	}

	indexed4, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("add file indexing failed: %v", err)
	}
	if indexed4.Stats.FilesAdded != 1 {
		t.Errorf("FilesAdded = %d, want 1", indexed4.Stats.FilesAdded)
	}
	if indexed4.Stats.ChunksEmbedded != 3 { // 1 struct + 2 methods
		t.Errorf("ChunksEmbedded = %d, want 3", indexed4.Stats.ChunksEmbedded)
	}

	// 5. Delete a file
	embedder.reset()
	if err := os.Remove(addedFile); err != nil {
		t.Fatalf("remove file error: %v", err)
	}
	indexed5, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("delete file indexing failed: %v", err)
	}
	if indexed5.Stats.FilesDeleted != 1 {
		t.Errorf("FilesDeleted = %d, want 1", indexed5.Stats.FilesDeleted)
	}
	if indexed5.Stats.ChunksEmbedded != 0 {
		t.Errorf("ChunksEmbedded = %d, want 0", indexed5.Stats.ChunksEmbedded)
	}

	// 6. Test Relationship Graph Invalidation & Update
	// Check initial relationship: CheckoutService.ProcessOrder -> PaymentGateway.ChargeCard
	tools := NewCodePilotTools(indexed5)
	callees, err := tools.FindCallees("ProcessOrder")
	if err != nil {
		t.Fatalf("FindCallees error: %v", err)
	}
	if len(callees) == 0 || callees[0].Name != "ChargeCard" {
		t.Fatalf("expected ProcessOrder to call ChargeCard, got: %v", callees)
	}

	// Now modify orders/checkout.go to call RefundCard instead!
	checkoutFile := filepath.Join(rootDir, "orders", "checkout.go")
	updatedCheckoutContent := `package orders

type CheckoutService struct {
	Gateway *PaymentGateway
}

func (c *CheckoutService) ProcessOrder(total int) bool {
	return c.Gateway.RefundCard(total)
}
`
	if err := os.WriteFile(checkoutFile, []byte(updatedCheckoutContent), 0o600); err != nil {
		t.Fatalf("update checkout error: %v", err)
	}

	indexed6, err := indexer.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("reindex after relationship update failed: %v", err)
	}

	toolsUpdated := NewCodePilotTools(indexed6)
	updatedCallees, err := toolsUpdated.FindCallees("ProcessOrder")
	if err != nil {
		t.Fatalf("FindCallees error: %v", err)
	}
	if len(updatedCallees) == 0 || updatedCallees[0].Name != "RefundCard" {
		t.Fatalf("expected ProcessOrder to call RefundCard after update, got: %v", updatedCallees)
	}
	for _, callee := range updatedCallees {
		if callee.Name == "ChargeCard" {
			t.Errorf("stale call relationship detected: ProcessOrder still references ChargeCard!")
		}
	}
	t.Log("[Scale Test - Relationship Graph] Invalidation verified: stale edges successfully purged and updated!")
}
