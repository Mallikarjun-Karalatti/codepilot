package benchmarks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"codepilot/internal/indexer"
	"codepilot/internal/retrieval"
)

type testEmbedderWithTracking struct {
	mu    sync.Mutex
	calls int
}

func (e *testEmbedderWithTracking) Embed(text string) ([]float64, error) {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	v := make([]float64, 4096)
	v[0] = 1.0
	return v, nil
}

func (e *testEmbedderWithTracking) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *testEmbedderWithTracking) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = 0
}

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
	store, err := indexer.NewFilesystemIndexStore(storeDir)
	if err != nil {
		t.Fatalf("NewFilesystemIndexStore error: %v", err)
	}

	embedder := &testEmbedderWithTracking{}
	config := indexer.DefaultIndexConfig()
	config.ParseWorkers = 8
	config.EmbedWorkers = 8

	idx := &indexer.IncrementalIndexer{
		Config:   config,
		Embedder: embedder,
		Store:    store,
	}

	// 1. Initial Indexing
	start := time.Now()
	indexed1, err := idx.Index(ctx, rootDir)
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
	indexed2, err := idx.Index(ctx, rootDir)
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
	indexed3, err := idx.Index(ctx, rootDir)
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
	addedFile := filepath.Join(rootDir, "users", "bonus_service.go")
	addedContent := `package users

type BonusService struct{}

func (b *BonusService) AwardBonus() int {
	return 100
}
`
	if err := os.WriteFile(addedFile, []byte(addedContent), 0o600); err != nil {
		t.Fatalf("write added file error: %v", err)
	}

	start = time.Now()
	indexed4, err := idx.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("add file indexing failed: %v", err)
	}
	addDuration := time.Since(start)

	if indexed4.Stats.FilesAdded != 1 {
		t.Errorf("FilesAdded = %d, want 1", indexed4.Stats.FilesAdded)
	}
	if indexed4.Stats.ChunksEmbedded != 2 { // struct + method
		t.Errorf("ChunksEmbedded = %d, want 2", indexed4.Stats.ChunksEmbedded)
	}
	t.Logf("[Scale Test - 1 File Added] Files Added: %d, Chunks Embedded: %d, Time: %v",
		indexed4.Stats.FilesAdded, indexed4.Stats.ChunksEmbedded, addDuration)

	// 5. Delete a file
	embedder.reset()
	deletedFile := filepath.Join(rootDir, "billing", "service_08.go")
	if err := os.Remove(deletedFile); err != nil {
		t.Fatalf("remove file error: %v", err)
	}

	start = time.Now()
	indexed5, err := idx.Index(ctx, rootDir)
	if err != nil {
		t.Fatalf("delete file indexing failed: %v", err)
	}
	deleteDuration := time.Since(start)

	if indexed5.Stats.FilesDeleted != 1 {
		t.Errorf("FilesDeleted = %d, want 1", indexed5.Stats.FilesDeleted)
	}
	if indexed5.Stats.ChunksEmbedded != 0 {
		t.Errorf("ChunksEmbedded = %d on delete, want 0", indexed5.Stats.ChunksEmbedded)
	}
	t.Logf("[Scale Test - 1 File Deleted] Files Deleted: %d, Chunks Kept: %d, Time: %v",
		indexed5.Stats.FilesDeleted, indexed5.Stats.ChunksKept, deleteDuration)

	// 6. Verify cross-file call relationship resolution across orders checkout & gateway
	repo, err := retrieval.NewIndexedRepository(indexed5, embedder)
	if err != nil {
		t.Fatalf("NewIndexedRepository error: %v", err)
	}
	var checkoutChunkID int
	for _, c := range repo.Chunks {
		if c.Name == "ProcessOrder" && c.Kind == indexer.ChunkKindMethod {
			checkoutChunkID = c.ID
			break
		}
	}
	if checkoutChunkID == 0 {
		t.Fatalf("ProcessOrder method not found in indexed chunks")
	}

	callees := repo.Graph.FindCallees(checkoutChunkID)
	foundGatewayCall := false
	for _, callee := range callees {
		if callee.Name == "ChargeCard" {
			foundGatewayCall = true
			break
		}
	}
	if !foundGatewayCall {
		t.Errorf("Cross-file AST relationship missing: ProcessOrder should call ChargeCard")
	} else {
		t.Logf("[Scale Test - Graph Integrity] Verified cross-file Call relationship from ProcessOrder -> ChargeCard")
	}
}
