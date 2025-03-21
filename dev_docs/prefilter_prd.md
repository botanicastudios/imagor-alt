# Prefilters Feature - Product Requirements Document

## Overview

The prefilters feature adds support for expensive image processing operations that are cached separately from regular filters. When an image URL includes a prefilter, the system first checks if a prefiltered version exists in prefilterStorage. If it does, that version is loaded and the rest of the filter chain is applied to it. If not, the prefilter operation is performed, the result is saved to prefilterStorage, and then the remaining filters are applied.

## Key Features

1. **Prefilter Processing Logic**

   - Prefilters are specific filters applied before the regular filter chain
   - Initial implementation includes `depthmap` filter
   - Prefilters act as intermediate processing steps with cached results
   - Prefilters can call external APIs for processing (not limited to Vips)

2. **Prefilter Storage**

   - Configure prefilterStorage similar to storage/resultStorage
   - Support for file, S3, Google Cloud storage backends
   - Reuse existing Storage interface implementation
   - Use existing hashing mechanisms (digest, suffix) through configuration

3. **Caching Strategy**
   - Only expensive operations should be designated as prefilters
   - Results are stored based on source image + prefilter parameters
   - Support for multiple prefilters in a single request
   - Ability to reuse partial prefilter results (e.g., reuse an image with removebg when processing removebg+depthmap)

## Acceptance Criteria

1. When a URL contains prefilters:

   - System checks prefilterStorage for cached versions
   - If found, loads the most applicable prefiltered version
   - If not found, processes the prefilters in sequence and saves results to prefilterStorage

2. Configuration:

   - PrefilterStorage configurable via same mechanisms as storage/resultStorage
   - Reuse of existing Storage interfaces
   - Support for path style configuration via IMAGOR_PREFILTER_STORAGE_PATH_STYLE

3. Performance:
   - Significant performance improvement for repeated requests with prefilters
   - Minimal overhead for first-time processing
   - Efficient handling of multiple prefilters

## Implementation Plan

### 1. Core Components to Modify

#### Imagor struct (imagor.go)

Add PrefilterStorages field to the Imagor struct:

```go
type Imagor struct {
    // existing fields
    PrefilterStorages         []Storage
    PrefilterStoragePathStyle imagorpath.PrefilterStorageHasher
    // other fields
}
```

#### Configuration Options (option.go)

Add WithPrefilterStorages option function:

```go
func WithPrefilterStorages(savers ...Storage) Option {
    return func(app *Imagor) {
        app.PrefilterStorages = append(app.PrefilterStorages, savers...)
    }
}

func WithPrefilterStoragePathStyle(hasher imagorpath.PrefilterStorageHasher) Option {
    return func(app *Imagor) {
        if hasher != nil {
            app.PrefilterStoragePathStyle = hasher
        }
    }
}
```

#### Path Handling (imagorpath package)

Extend PrefilterStorageHasher similar to ResultStorageHasher:

```go
type PrefilterStorageHasher interface {
    Hash(params Params, image string, prefilters []PrefilterDefinition) (string, error)
}

type PrefilterDefinition struct {
    Name string
    Args string
}
```

### 2. Processing Flow Implementation

#### Prefilter Detection

Modify the processing flow in the `Do` method of Imagor to:

1. Parse the URL and detect prefilters
2. Determine the optimal chain of prefilters to apply
3. For each step, check if it exists in prefilterStorage
4. If found, load it and continue processing
5. If not found, apply the prefilter, save to prefilterStorage, and continue

#### Prefilter Storage Logic

Create methods for handling prefilter storage:

```go
func (app *Imagor) loadPrefilter(r *http.Request, prefilterKey string) (*Blob, error)
func (app *Imagor) savePrefilter(ctx context.Context, prefilterKey string, blob *Blob) error
func (app *Imagor) applyPrefilter(ctx context.Context, blob *Blob, prefilterName, prefilterArgs string) (*Blob, error)
```

### 3. Prefilter Implementation

#### Prefilter Interface

Create a dedicated prefilter interface for both internal and external processing:

```go
type Prefilter interface {
    Name() string
    Apply(ctx context.Context, blob *Blob, args string) (*Blob, error)
}
```

#### HTTP-based Prefilters

Implement an HTTP client for external API-based prefilters:

```go
type HTTPPrefilter struct {
    name    string
    apiURL  string
    client  *http.Client
    timeout time.Duration
}

func (p *HTTPPrefilter) Apply(ctx context.Context, blob *Blob, args string) (*Blob, error) {
    // Prepare request to external API
    // Send image data
    // Process response
    // Return new blob
}
```

#### Initial Prefilters

Implement the `depthmap` prefilter using an HTTP client:

```go
func NewDepthmapPrefilter(apiURL string, timeout time.Duration) *HTTPPrefilter {
    return &HTTPPrefilter{
        name:    "depthmap",
        apiURL:  apiURL,
        client:  &http.Client{},
        timeout: timeout,
    }
}
```

### 4. Multiple Prefilters Handling

#### Identifying Prefilters

Create a mechanism to identify prefilters in filter chains:

```go
func IsPrefilter(filterName string) bool {
    prefilters := map[string]bool{
        "depthmap": true,
        "removebg": true,
        // add more prefilters here
    }
    return prefilters[filterName]
}
```

#### Prefilter Chain Optimization

Implement logic to optimize prefilter chains to maximize cache hits: prefilters should be applied in a specified order regardless of the order they appear in the filters string in the current request URL.

#### Result Handling

Ensure proper error handling and fallback mechanisms if a prefilter fails.

## Technical Considerations

1. **Key Generation**

   - Keys for prefilterStorage should take into account the source image path and all prefilter parameters
   - Support for IMAGOR_PREFILTER_STORAGE_PATH_STYLE with options like "digest" or "suffix"
   - Format depends on chosen hash style (consistent with other storage mechanisms)

2. **Error Handling**

   - If prefilterStorage access fails, fall back to processing without cached results
   - If an external API fails, provide clear error messages and appropriate status codes
   - Log detailed error information for debugging

## Testing Strategy

2. **Integration Tests**

   - Test the full prefilter processing pipeline
   - Test interaction with external APIs (with mocks)
   - Test storage and retrieval of prefiltered images

3. **Mock External APIs**

   - Create mock servers for external prefilter APIs
   - Simulate various response scenarios (success, failure, timeout)

4. **End-to-End Tests**

   - Test the entire system with real-world image processing scenarios
   - Verify correct behavior with multiple prefilters
   - Confirm that the cache works as expected over multiple requests
