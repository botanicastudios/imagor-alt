package imagor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Prefilter interface for image prefiltering operations
type Prefilter interface {
	// Name returns the unique name of the prefilter
	Name() string

	// Apply processes the image blob with the given arguments
	Apply(ctx context.Context, blob *Blob, args string) (*Blob, error)
}

// HTTPPrefilter implements the Prefilter interface using an HTTP API
type HTTPPrefilter struct {
	name    string
	apiURL  string
	client  *http.Client
	timeout time.Duration
}

// Name returns the prefilter name
func (p *HTTPPrefilter) Name() string {
	return p.name
}

// Apply sends the image to an external API for processing
func (p *HTTPPrefilter) Apply(ctx context.Context, blob *Blob, args string) (*Blob, error) {
	if p.apiURL == "" {
		return nil, fmt.Errorf("no API URL configured for prefilter %s", p.name)
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	// Prepare the request data
	data := url.Values{}

	// If the blob has a source URL, provide it to the API
	if blob.Header != nil && blob.Header.Get("source_url") != "" {
		data.Set("source_url", blob.Header.Get("source_url"))
	} else {
		// If no source URL, upload the image data
		// For this, we need to convert the request to a multipart form
		return p.uploadAndProcess(ctx, blob, args)
	}

	// Add any arguments if provided
	if args != "" {
		data.Set("args", args)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request for prefilter %s: %w", p.name, err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Imagor-Prefilter/"+Version)

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to prefilter %s: %w", p.name, err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Try to read error message from response
		var errorResponse struct {
			Error string `json:"error"`
		}

		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &errorResponse); err == nil && errorResponse.Error != "" {
			return nil, fmt.Errorf("prefilter %s API error: %s", p.name, errorResponse.Error)
		}

		return nil, fmt.Errorf("prefilter %s API returned status %d", p.name, resp.StatusCode)
	}

	// Read and process the response
	resultData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from prefilter %s: %w", p.name, err)
	}

	// Create a new blob with the result
	resultBlob := NewBlobFromBytes(resultData)

	// Copy metadata from the original blob
	if blob.Header != nil {
		if resultBlob.Header == nil {
			resultBlob.Header = http.Header{}
		}
		for k, v := range blob.Header {
			for _, val := range v {
				resultBlob.Header.Add(k, val)
			}
		}
	}

	// Set the content type based on the response or keep the original if not specified
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		resultBlob.SetContentType(contentType)
	} else {
		resultBlob.SetContentType(blob.ContentType())
	}

	return resultBlob, nil
}

// uploadAndProcess handles the case where we need to upload the image data to the API
func (p *HTTPPrefilter) uploadAndProcess(ctx context.Context, blob *Blob, args string) (*Blob, error) {
	// For simplicity, we'll use a direct raw body upload
	// In a production implementation, you might want to use multipart/form-data

	// Read the blob data
	blobData, err := blob.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read blob data: %w", err)
	}

	// Create HTTP request with the image data as the body
	req, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, bytes.NewReader(blobData))
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request for prefilter %s: %w", p.name, err)
	}

	// Set headers
	req.Header.Set("Content-Type", blob.ContentType())
	req.Header.Set("User-Agent", "Imagor-Prefilter/"+Version)

	// Add arguments as query parameters if provided
	if args != "" {
		q := req.URL.Query()
		q.Add("args", args)
		req.URL.RawQuery = q.Encode()
	}

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to upload to prefilter %s: %w", p.name, err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Try to read error message from response
		var errorResponse struct {
			Error string `json:"error"`
		}

		body, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(body, &errorResponse); err == nil && errorResponse.Error != "" {
			return nil, fmt.Errorf("prefilter %s API error: %s", p.name, errorResponse.Error)
		}

		return nil, fmt.Errorf("prefilter %s API returned status %d", p.name, resp.StatusCode)
	}

	// Read and process the response
	resultData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response from prefilter %s: %w", p.name, err)
	}

	// Create a new blob with the result
	resultBlob := NewBlobFromBytes(resultData)

	// Copy metadata from the original blob
	if blob.Header != nil {
		if resultBlob.Header == nil {
			resultBlob.Header = http.Header{}
		}
		for k, v := range blob.Header {
			for _, val := range v {
				resultBlob.Header.Add(k, val)
			}
		}
	}

	// Set the content type based on the response or keep the original if not specified
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		resultBlob.SetContentType(contentType)
	} else {
		resultBlob.SetContentType(blob.ContentType())
	}

	return resultBlob, nil
}

// NewDepthmapPrefilter creates a new depthmap prefilter
func NewDepthmapPrefilter(apiURL string, timeout time.Duration) *HTTPPrefilter {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &HTTPPrefilter{
		name:    "depthmap",
		apiURL:  apiURL,
		client:  &http.Client{},
		timeout: timeout,
	}
}

// IsPrefilter checks if a filter name is a prefilter
func IsPrefilter(filterName string) bool {
	prefilters := map[string]bool{
		"depthmap": true,
		"removebg": true,
		// Add more prefilters here as they are implemented
	}
	return prefilters[filterName]
}
