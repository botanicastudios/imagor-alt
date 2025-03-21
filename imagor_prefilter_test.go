package imagor_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cshum/imagor"
	"github.com/cshum/imagor/imagorpath"
)

// mockStorage implements Storage interface for testing
type mockStorage struct {
	store map[string][]byte
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		store: make(map[string][]byte),
	}
}

func (s *mockStorage) Get(_ *http.Request, image string) (*imagor.Blob, error) {
	if data, ok := s.store[image]; ok {
		return imagor.NewBlobFromBytes(data), nil
	}
	return nil, imagor.ErrNotFound
}

func (s *mockStorage) Put(_ context.Context, image string, blob *imagor.Blob) error {
	data, err := blob.ReadAll()
	if err != nil {
		return err
	}
	s.store[image] = data
	return nil
}

func (s *mockStorage) Delete(_ context.Context, image string) error {
	delete(s.store, image)
	return nil
}

func (s *mockStorage) Stat(_ context.Context, image string) (*imagor.Stat, error) {
	if _, ok := s.store[image]; ok {
		return &imagor.Stat{
			ModifiedTime: time.Now(),
		}, nil
	}
	return nil, imagor.ErrNotFound
}

func TestPrefilter(t *testing.T) {
	// Create test server for depthmap API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST request, got %s", r.Method)
			return
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("failed to parse form: %v", err)
			return
		}
		sourceURL := r.FormValue("source_url")
		if sourceURL == "" {
			t.Error("expected source_url in form data")
			return
		}
		// Return test depth map image
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("depthmap data"))
	}))
	defer server.Close()

	// Create test storage
	tmpDir := t.TempDir()
	storage := newMockStorage()
	prefilterStorage := newMockStorage()

	// Create test image
	testImage := "test.jpg"
	testImagePath := filepath.Join(tmpDir, testImage)
	if err := os.WriteFile(testImagePath, []byte("test image data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create Imagor instance with prefilter
	app := imagor.New(
		imagor.WithStorages(storage),
		imagor.WithPrefilterStorages(prefilterStorage),
		imagor.WithPrefilterStoragePathStyle(imagorpath.SuffixPrefilterStorageHasher),
	)

	// Add depthmap prefilter
	depthmapPrefilter := imagor.NewDepthmapPrefilter(server.URL, time.Second*5)
	app.Processors = append(app.Processors, depthmapPrefilter)

	// Test prefilter processing
	t.Run("process depthmap prefilter", func(t *testing.T) {
		params := imagorpath.Params{
			Image: testImage,
			Filters: []imagorpath.Filter{
				{Name: "depthmap", Args: ""},
				{Name: "resize", Args: "100x100"},
			},
		}

		blob, err := app.Do(nil, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if blob == nil {
			t.Fatal("expected non-nil blob")
		}

		// Verify prefilter storage
		prefilterKey := imagorpath.SuffixPrefilterStorageHasher.Hash(params, testImage, []imagorpath.PrefilterDefinition{
			{Name: "depthmap", Args: ""},
		})
		if _, err := prefilterStorage.Get(nil, prefilterKey); err != nil {
			t.Errorf("expected prefilter result in storage: %v", err)
		}
	})

	// Test prefilter caching
	t.Run("reuse cached prefilter", func(t *testing.T) {
		params := imagorpath.Params{
			Image: testImage,
			Filters: []imagorpath.Filter{
				{Name: "depthmap", Args: ""},
				{Name: "resize", Args: "200x200"},
			},
		}

		// First request should hit the API
		blob1, err := app.Do(nil, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Second request should use cached result
		blob2, err := app.Do(nil, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify both blobs are the same
		reader1, _, _ := blob1.NewReader()
		reader2, _, _ := blob2.NewReader()
		data1, _ := io.ReadAll(reader1)
		data2, _ := io.ReadAll(reader2)
		if !bytes.Equal(data1, data2) {
			t.Error("expected cached prefilter result to match")
		}
	})

	// Test multiple prefilters
	t.Run("process multiple prefilters", func(t *testing.T) {
		params := imagorpath.Params{
			Image: testImage,
			Filters: []imagorpath.Filter{
				{Name: "depthmap", Args: ""},
				{Name: "removebg", Args: ""},
				{Name: "resize", Args: "300x300"},
			},
		}

		blob, err := app.Do(nil, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if blob == nil {
			t.Fatal("expected non-nil blob")
		}

		// Verify prefilter storage for both prefilters
		prefilterKey1 := imagorpath.SuffixPrefilterStorageHasher.Hash(params, testImage, []imagorpath.PrefilterDefinition{
			{Name: "depthmap", Args: ""},
		})
		if _, err := prefilterStorage.Get(nil, prefilterKey1); err != nil {
			t.Errorf("expected depthmap prefilter result in storage: %v", err)
		}

		prefilterKey2 := imagorpath.SuffixPrefilterStorageHasher.Hash(params, testImage, []imagorpath.PrefilterDefinition{
			{Name: "depthmap", Args: ""},
			{Name: "removebg", Args: ""},
		})
		if _, err := prefilterStorage.Get(nil, prefilterKey2); err != nil {
			t.Errorf("expected removebg prefilter result in storage: %v", err)
		}
	})
}
