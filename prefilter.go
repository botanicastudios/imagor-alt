package imagor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

	// Create a context with the configured timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	// If the blob has a source URL, provide it to the API
	if blob.Header != nil && blob.Header.Get("image_url") != "" {
		// Create request payload as JSON
		requestData := map[string]string{
			"image_url": blob.Header.Get("image_url"),
		}

		// Add any arguments if provided
		if args != "" {
			requestData["args"] = args
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(requestData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal JSON for prefilter %s: %w", p.name, err)
		}

		// Create HTTP request with the timeout context
		req, err := http.NewRequestWithContext(timeoutCtx, "POST", p.apiURL, bytes.NewReader(jsonData))
		if err != nil {
			return nil, fmt.Errorf("failed to create request for prefilter %s: %w", p.name, err)
		}

		// Set headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Imagor-Prefilter/"+Version)

		// Send request
		resp, err := p.client.Do(req)

		// Handle errors
		if err != nil {
			// Check if it's a timeout
			if errors.Is(err, context.DeadlineExceeded) || timeoutCtx.Err() == context.DeadlineExceeded {
				return nil, ErrTimeout
			}

			// Check for network timeout errors
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return nil, ErrTimeout
			}

			return nil, fmt.Errorf("failed to send request to prefilter %s: %w", p.name, err)
		}

		if resp == nil {
			return nil, fmt.Errorf("prefilter %s returned nil response", p.name)
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

		// Check for empty response
		if len(resultData) == 0 {
			return nil, fmt.Errorf("prefilter %s returned empty response", p.name)
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
	} else {
		// If no source URL, upload the image data
		return p.uploadAndProcess(timeoutCtx, blob, args)
	}
}

// uploadAndProcess handles the case where we need to upload the image data to the API
func (p *HTTPPrefilter) uploadAndProcess(ctx context.Context, blob *Blob, args string) (*Blob, error) {
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

	// Handle errors
	if err != nil {
		// Check if it's a timeout
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}

		// Check for network timeout errors
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, ErrTimeout
		}

		return nil, fmt.Errorf("failed to upload to prefilter %s: %w", p.name, err)
	}

	if resp == nil {
		return nil, fmt.Errorf("prefilter %s returned nil response", p.name)
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

	// Check for empty response
	if len(resultData) == 0 {
		return nil, fmt.Errorf("prefilter %s returned empty response", p.name)
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
		timeout = 60 * time.Second // Use a sensible default
	}

	// Create client with timeout
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
		},
	}

	return &HTTPPrefilter{
		name:    "depthmap",
		apiURL:  apiURL,
		client:  client,
		timeout: timeout,
	}
}

// NewRemoveBgPrefilter creates a new background removal prefilter
func NewRemoveBgPrefilter(apiURL string, timeout time.Duration) *HTTPPrefilter {
	if timeout <= 0 {
		timeout = 60 * time.Second // Use a sensible default
	}

	// Create client with timeout
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
		},
	}

	return &HTTPPrefilter{
		name:    "removebg",
		apiURL:  apiURL,
		client:  client,
		timeout: timeout,
	}
}

// NewAIUpscalePrefilter creates a new AI upscale prefilter
func NewAIUpscalePrefilter(apiURL string, timeout time.Duration) *HTTPPrefilter {
	if timeout <= 0 {
		timeout = 60 * time.Second // Use a sensible default
	}

	// Create client with timeout
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
		},
	}

	return &HTTPPrefilter{
		name:    "ai_upscale",
		apiURL:  apiURL,
		client:  client,
		timeout: timeout,
	}
}

// IsPrefilter checks if a filter name is a prefilter
func IsPrefilter(filterName string) bool {
	prefilters := map[string]bool{
		"depthmap":   true,
		"removebg":   true,
		"ai_upscale": true,
		// Add more prefilters here as they are implemented
	}
	return prefilters[filterName]
}
