package imagor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cshum/imagor/imagorpath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockPrefilter implements Prefilter for testing
type MockPrefilter struct {
	name      string
	mockApply func(ctx context.Context, blob *Blob, args string) (*Blob, error)
}

func (p *MockPrefilter) Name() string {
	return p.name
}

func (p *MockPrefilter) Apply(ctx context.Context, blob *Blob, args string) (*Blob, error) {
	return p.mockApply(ctx, blob, args)
}

// Setup a mock depth map prefilter that uses test files
func NewMockDepthmapPrefilter() *MockPrefilter {
	return &MockPrefilter{
		name: "depthmap",
		mockApply: func(ctx context.Context, blob *Blob, args string) (*Blob, error) {
			// For testing, we'll just return the sample depth map file
			depthMapData, err := os.ReadFile("testdata/depth_map.png")
			if err != nil {
				return nil, err
			}

			result := NewBlobFromBytes(depthMapData)
			result.SetContentType("image/png")

			// Copy original metadata
			if blob.Header != nil {
				if result.Header == nil {
					result.Header = make(http.Header)
				}
				for k, v := range blob.Header {
					for _, val := range v {
						result.Header.Add(k, val)
					}
				}
			}

			return result, nil
		},
	}
}

// MockFileStorage is a simple in-memory storage implementation for testing
type MockFileStorage struct {
	data     map[string][]byte
	metadata map[string]map[string]string
	baseDir  string
	mu       sync.RWMutex
}

func NewMockFileStorage(baseDir string) *MockFileStorage {
	return &MockFileStorage{
		data:     make(map[string][]byte),
		metadata: make(map[string]map[string]string),
		baseDir:  baseDir,
	}
}

func (s *MockFileStorage) Get(_ *http.Request, key string) (*Blob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.data[key]
	if !ok {
		return nil, ErrNotFound
	}

	blob := NewBlobFromBytes(data)

	// Set metadata if it exists
	if meta, ok := s.metadata[key]; ok && len(meta) > 0 {
		if blob.Header == nil {
			blob.Header = make(http.Header)
		}
		for k, v := range meta {
			blob.Header.Set(k, v)
		}
	}

	return blob, nil
}

func (s *MockFileStorage) Put(_ context.Context, key string, blob *Blob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blobData, err := blob.ReadAll()
	if err != nil {
		return err
	}
	s.data[key] = blobData

	// Store metadata
	s.metadata[key] = make(map[string]string)
	if blob.Header != nil {
		for k, vv := range blob.Header {
			if len(vv) > 0 {
				s.metadata[key][k] = vv[0]
			}
		}
	}

	return nil
}

func (s *MockFileStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	delete(s.metadata, key)
	return nil
}

func (s *MockFileStorage) Stat(_ context.Context, key string) (*Stat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.data[key]
	if !ok {
		return nil, ErrNotFound
	}

	return &Stat{
		Size:         int64(len(data)),
		ModifiedTime: time.Now(),
	}, nil
}

// Mock processor for testing
type MockProcessor struct {
	mockProcess func(ctx context.Context, blob *Blob, p imagorpath.Params, load LoadFunc) (*Blob, error)
}

func (m *MockProcessor) Process(ctx context.Context, blob *Blob, p imagorpath.Params, load LoadFunc) (*Blob, error) {
	return m.mockProcess(ctx, blob, p, load)
}

func (m *MockProcessor) Startup(ctx context.Context) error {
	return nil
}

func (m *MockProcessor) Shutdown(ctx context.Context) error {
	return nil
}

// Mock loader for testing
type MockLoader struct {
	mockGet func(r *http.Request, key string) (*Blob, error)
}

func (m *MockLoader) Get(r *http.Request, key string) (*Blob, error) {
	return m.mockGet(r, key)
}

func TestPrefilterInterface(t *testing.T) {
	mockPrefilter := &MockPrefilter{
		name: "testprefilter",
		mockApply: func(ctx context.Context, blob *Blob, args string) (*Blob, error) {
			return blob, nil // just return the same blob for the test
		},
	}

	assert.Equal(t, "testprefilter", mockPrefilter.Name())

	blob := NewBlobFromBytes([]byte("test"))
	resultBlob, err := mockPrefilter.Apply(context.Background(), blob, "")

	assert.NoError(t, err)
	assert.Equal(t, blob, resultBlob)
}

func TestDepthmapPrefilter(t *testing.T) {
	mockPrefilter := NewMockDepthmapPrefilter()

	assert.Equal(t, "depthmap", mockPrefilter.Name())

	// Read the test image
	imgData, err := os.ReadFile("testdata/frog.jpg")
	require.NoError(t, err)

	blob := NewBlobFromBytes(imgData)
	blob.SetContentType("image/jpeg")

	// Apply the prefilter
	resultBlob, err := mockPrefilter.Apply(context.Background(), blob, "")

	assert.NoError(t, err)
	assert.NotNil(t, resultBlob)
	assert.Equal(t, "image/png", resultBlob.ContentType())

	// Compare with the expected depth map
	expectedData, err := os.ReadFile("testdata/depth_map.png")
	require.NoError(t, err)

	resultData, err := resultBlob.ReadAll()
	require.NoError(t, err)
	assert.Equal(t, expectedData, resultData)
}

func TestIsPrefilter(t *testing.T) {
	assert.True(t, IsPrefilter("depthmap"))
	assert.True(t, IsPrefilter("removebg"))
	assert.False(t, IsPrefilter("blur"))
	assert.False(t, IsPrefilter("format"))
}

func TestExtractPrefilters(t *testing.T) {
	filters := []imagorpath.Filter{
		{Name: "depthmap", Args: ""},
		{Name: "blur", Args: "10"},
		{Name: "removebg", Args: "person"},
		{Name: "format", Args: "webp"},
	}

	prefilters, regularFilters := extractPrefilters(filters)

	assert.Len(t, prefilters, 2)
	assert.Len(t, regularFilters, 2)

	assert.Equal(t, "depthmap", prefilters[0].Name)
	assert.Equal(t, "removebg", prefilters[1].Name)
	assert.Equal(t, "person", prefilters[1].Args)

	assert.Equal(t, "blur", regularFilters[0].Name)
	assert.Equal(t, "format", regularFilters[1].Name)
}

func TestPrefilterStorage(t *testing.T) {
	tempDir := t.TempDir()

	// Create a mock file storage for prefilters
	storage := NewMockFileStorage(tempDir)

	// Create the Imagor app with prefilter storage
	app := New(
		WithPrefilterStorages(storage),
		WithPrefilterStoragePathStyle(imagorpath.DigestPrefilterStorageHasher),
		WithPrefilters(NewMockDepthmapPrefilter()),
	)

	// Test image
	imgPath := "testdata/frog.jpg"
	imgData, err := os.ReadFile(imgPath)
	require.NoError(t, err)

	// Create a test blob
	blob := NewBlobFromBytes(imgData)
	blob.SetContentType("image/jpeg")

	// Create a prefilter key
	prefilters := []imagorpath.PrefilterDefinition{
		{Name: "depthmap", Args: ""},
	}
	prefilterKey := imagorpath.DigestPrefilterStorageHasher.Hash(imagorpath.Params{}, imgPath, prefilters)

	// Save the prefilter result
	ctx := context.Background()
	err = app.savePrefilter(ctx, prefilterKey, blob)
	assert.NoError(t, err)

	// Wait for async save to complete
	time.Sleep(100 * time.Millisecond)

	// Load the prefilter result
	r, _ := http.NewRequest("GET", "/", nil)
	loadedBlob, err := app.loadPrefilter(r, prefilterKey)
	assert.NoError(t, err)
	assert.NotNil(t, loadedBlob)

	loadedData, err := loadedBlob.ReadAll()
	require.NoError(t, err)
	assert.Equal(t, imgData, loadedData)
}

func TestImageProcessingWithPrefilter(t *testing.T) {
	// Create a mock depthmap prefilter
	mockPrefilter := NewMockDepthmapPrefilter()

	// Create test image data
	imgData, err := os.ReadFile("testdata/frog.jpg")
	require.NoError(t, err)

	// Create a blob with the test image
	blob := NewBlobFromBytes(imgData)
	blob.SetContentType("image/jpeg")

	// Apply the prefilter
	ctx := context.Background()
	resultBlob, err := mockPrefilter.Apply(ctx, blob, "")

	// Verify the result
	require.NoError(t, err)
	require.NotNil(t, resultBlob)
	assert.Equal(t, "image/png", resultBlob.ContentType())

	// Compare with the expected depth map
	expectedData, err := os.ReadFile("testdata/depth_map.png")
	require.NoError(t, err)

	resultData, err := resultBlob.ReadAll()
	require.NoError(t, err)
	assert.Equal(t, expectedData, resultData)
}

// Test HTTP Prefilter implementation
func TestHTTPPrefilter(t *testing.T) {
	// Create a test server that responds with a modified image
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that we received the correct request
		assert.Equal(t, "POST", r.Method)

		// Verify that content type is application/json when image_url is sent
		contentType := r.Header.Get("Content-Type")
		if r.Header.Get("Content-Type") == "application/json" {
			// Read the uploaded JSON
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.NotEmpty(t, body)

			// Verify it's valid JSON with the expected structure
			var requestData map[string]string
			err = json.Unmarshal(body, &requestData)
			assert.NoError(t, err)
			assert.Contains(t, requestData, "image_url")
			assert.Equal(t, "http://example.com/test.jpg", requestData["image_url"])
		} else {
			// For direct image upload, the content-type should be different
			assert.NotEmpty(t, contentType)

			// Read the uploaded image
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.NotEmpty(t, body)
		}

		// Return the depth map image
		depthMapData, _ := os.ReadFile("testdata/depth_map.png")
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(depthMapData)
	}))
	defer server.Close()

	// Create the HTTP prefilter
	prefilter := NewDepthmapPrefilter(server.URL, 5*time.Second)
	assert.Equal(t, "depthmap", prefilter.Name())

	// Load test image
	imgData, err := os.ReadFile("testdata/frog.jpg")
	require.NoError(t, err)

	blob := NewBlobFromBytes(imgData)
	blob.SetContentType("image/jpeg")

	// Test with source URL
	if blob.Header == nil {
		blob.Header = make(http.Header)
	}
	blob.Header.Set("image_url", "http://example.com/test.jpg")

	// Apply the prefilter
	ctx := context.Background()
	resultBlob, err := prefilter.Apply(ctx, blob, "")

	assert.NoError(t, err)
	assert.NotNil(t, resultBlob)
	assert.Equal(t, "image/png", resultBlob.ContentType())

	// Test without source URL (direct upload)
	blob.Header.Del("image_url")

	resultBlob2, err := prefilter.Apply(ctx, blob, "")

	assert.NoError(t, err)
	assert.NotNil(t, resultBlob2)
	assert.Equal(t, "image/png", resultBlob2.ContentType())
}
