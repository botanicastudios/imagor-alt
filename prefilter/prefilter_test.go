// Package prefilter provides prefilter implementations for imagor
package prefilter

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// Create test server for prefilters
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST request, got %s", r.Method)
			return
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("failed to parse form: %v", err)
			return
		}

		// Check if we have a source_url in form data
		sourceURL := r.FormValue("source_url")
		if sourceURL == "" {
			// Try getting the file if source_url is not present
			file, _, err := r.FormFile("image")
			if err != nil {
				t.Errorf("failed to get image file or source_url: %v", err)
				return
			}
			defer file.Close()
		}

		// Return different responses based on the request path
		if strings.Contains(r.URL.Path, "/depthmap") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("depthmap data"))
			return
		} else if strings.Contains(r.URL.Path, "/removebg") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("removebg data"))
			return
		}

		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("invalid prefilter"))
	}))
	defer server.Close()

	// Create test storage
	storage := newMockStorage()
	prefilterStorage := newMockStorage()

	// Create test image
	testImage := "test.jpg"
	testData := []byte("test image data")

	// Store test image in mock storage
	if err := storage.Put(context.Background(), testImage, imagor.NewBlobFromBytes(testData)); err != nil {
		t.Fatal(err)
	}

	// Create DepthmapPrefilter
	depthmapPrefilter := NewDepthmapPrefilter(server.URL+"/depthmap", time.Second*5)
	removebgPrefilter := NewRemoveBGPrefilter(server.URL+"/removebg", time.Second*5)

	// Make sure these implement both Processor and Prefilter interfaces
	var _ imagor.Processor = depthmapPrefilter
	var _ imagor.Prefilter = depthmapPrefilter
	var _ imagor.Processor = removebgPrefilter
	var _ imagor.Prefilter = removebgPrefilter

	// Create Imagor instance with prefilter
	app := imagor.New(
		imagor.WithStorages(storage),
		imagor.WithPrefilterStorages(prefilterStorage),
		imagor.WithPrefilterStoragePathStyle(imagorpath.SuffixPrefilterStorageHasher),
		imagor.WithUnsafe(true), // Allow unsafe URLs for testing
	)

	// Add processors
	app.Processors = append(app.Processors, depthmapPrefilter, removebgPrefilter)

	// Test prefilter processing
	t.Run("process depthmap prefilter", func(t *testing.T) {
		params := imagorpath.Params{
			Image: testImage,
			Filters: []imagorpath.Filter{
				{Name: "depthmap", Args: ""},
				{Name: "resize", Args: "100x100"},
			},
			Unsafe: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		blob, err := app.Do(req, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if blob == nil {
			t.Fatal("expected non-nil blob")
		}

		// Set source URL in the header for proper prefilter key generation
		if blob.Header == nil {
			blob.Header = make(http.Header)
		}
		blob.Header.Set("X-Source-URL", testImage)

		// Read the blob data
		reader, _, err := blob.NewReader()
		if err != nil {
			t.Fatalf("failed to get reader from blob: %v", err)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read data: %v", err)
		}
		if string(data) != "depthmap data" {
			t.Error("expected depthmap data response")
		}

		// Verify prefilter storage
		prefilterKey := imagorpath.SuffixPrefilterStorageHasher.Hash(params, testImage, []imagorpath.PrefilterDefinition{
			{Name: "depthmap", Args: ""},
		})
		storedBlob, err := prefilterStorage.Get(nil, prefilterKey)
		if err != nil {
			t.Errorf("expected prefilter result in storage: %v", err)
			return
		}
		if storedBlob == nil {
			t.Error("expected non-nil blob from prefilter storage")
			return
		}
		storedReader, _, err := storedBlob.NewReader()
		if err != nil {
			t.Errorf("failed to get reader from stored blob: %v", err)
			return
		}
		defer storedReader.Close()
		storedData, err := io.ReadAll(storedReader)
		if err != nil {
			t.Errorf("failed to read stored data: %v", err)
			return
		}
		if string(storedData) != "depthmap data" {
			t.Error("expected depthmap data in storage")
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
			Unsafe: true,
		}

		// First request should hit the API
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		blob1, err := app.Do(req, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Set source URL in the header for proper prefilter key generation
		if blob1.Header == nil {
			blob1.Header = make(http.Header)
		}
		blob1.Header.Set("X-Source-URL", testImage)

		// Second request should use cached result
		req = httptest.NewRequest(http.MethodGet, "/", nil)
		blob2, err := app.Do(req, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Set source URL in the header for proper prefilter key generation
		if blob2.Header == nil {
			blob2.Header = make(http.Header)
		}
		blob2.Header.Set("X-Source-URL", testImage)

		// Verify both blobs are the same
		reader1, _, err := blob1.NewReader()
		if err != nil {
			t.Fatalf("failed to get reader from blob1: %v", err)
		}
		defer reader1.Close()
		reader2, _, err := blob2.NewReader()
		if err != nil {
			t.Fatalf("failed to get reader from blob2: %v", err)
		}
		defer reader2.Close()
		data1, err := io.ReadAll(reader1)
		if err != nil {
			t.Fatalf("failed to read data1: %v", err)
		}
		data2, err := io.ReadAll(reader2)
		if err != nil {
			t.Fatalf("failed to read data2: %v", err)
		}
		if !bytes.Equal(data1, data2) {
			t.Error("expected cached prefilter result to match")
		}
		if string(data1) != "depthmap data" {
			t.Error("expected depthmap data response")
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
			Unsafe: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		blob, err := app.Do(req, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if blob == nil {
			t.Fatal("expected non-nil blob")
		}

		// Set source URL in the header for proper prefilter key generation
		if blob.Header == nil {
			blob.Header = make(http.Header)
		}
		blob.Header.Set("X-Source-URL", testImage)

		// Read the final blob data
		reader, _, err := blob.NewReader()
		if err != nil {
			t.Fatalf("failed to get reader from blob: %v", err)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("failed to read data: %v", err)
		}
		if string(data) != "removebg data" {
			t.Error("expected removebg data as final response")
		}

		// Verify prefilter storage for both prefilters
		prefilterKey1 := imagorpath.SuffixPrefilterStorageHasher.Hash(params, testImage, []imagorpath.PrefilterDefinition{
			{Name: "depthmap", Args: ""},
		})
		storedBlob1, err := prefilterStorage.Get(nil, prefilterKey1)
		if err != nil {
			t.Errorf("expected depthmap prefilter result in storage: %v", err)
			return
		}
		if storedBlob1 == nil {
			t.Error("expected non-nil depthmap blob from prefilter storage")
			return
		}
		storedReader1, _, err := storedBlob1.NewReader()
		if err != nil {
			t.Fatalf("failed to get reader from storedBlob1: %v", err)
		}
		defer storedReader1.Close()
		storedData1, err := io.ReadAll(storedReader1)
		if err != nil {
			t.Fatalf("failed to read storedData1: %v", err)
		}
		if string(storedData1) != "depthmap data" {
			t.Error("expected depthmap data in storage")
		}

		prefilterKey2 := imagorpath.SuffixPrefilterStorageHasher.Hash(params, testImage, []imagorpath.PrefilterDefinition{
			{Name: "depthmap", Args: ""},
			{Name: "removebg", Args: ""},
		})
		storedBlob2, err := prefilterStorage.Get(nil, prefilterKey2)
		if err != nil {
			t.Errorf("expected removebg prefilter result in storage: %v", err)
			return
		}
		if storedBlob2 == nil {
			t.Error("expected non-nil removebg blob from prefilter storage")
			return
		}
		storedReader2, _, err := storedBlob2.NewReader()
		if err != nil {
			t.Fatalf("failed to get reader from storedBlob2: %v", err)
		}
		defer storedReader2.Close()
		storedData2, err := io.ReadAll(storedReader2)
		if err != nil {
			t.Fatalf("failed to read storedData2: %v", err)
		}
		if string(storedData2) != "removebg data" {
			t.Error("expected removebg data in storage")
		}
	})

	// Test prefilter not found
	t.Run("prefilter not found", func(t *testing.T) {
		params := imagorpath.Params{
			Image: testImage,
			Filters: []imagorpath.Filter{
				{Name: "nonexistent", Args: ""},
				{Name: "resize", Args: "100x100"},
			},
			Unsafe: true,
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		_, err := app.Do(req, params)
		if err == nil {
			t.Fatal("expected error for nonexistent prefilter")
		}
		if !strings.Contains(err.Error(), "prefilter nonexistent not found") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
