// Package prefilter provides prefilter implementations
package prefilter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/cshum/imagor"
	"github.com/cshum/imagor/imagorpath"
)

// HTTPPrefilter implements both Processor and Prefilter interfaces
type HTTPPrefilter struct {
	name    string
	apiURL  string
	client  *http.Client
	timeout time.Duration
}

func (p *HTTPPrefilter) Name() string {
	return p.name
}

// Process implements Processor interface
func (p *HTTPPrefilter) Process(ctx context.Context, blob *imagor.Blob, params imagorpath.Params, load imagor.LoadFunc) (*imagor.Blob, error) {
	return p.Apply(ctx, blob, "")
}

// Startup implements Processor interface
func (p *HTTPPrefilter) Startup(ctx context.Context) error {
	return nil
}

// Shutdown implements Processor interface
func (p *HTTPPrefilter) Shutdown(ctx context.Context) error {
	return nil
}

// Apply implements Prefilter interface
func (p *HTTPPrefilter) Apply(ctx context.Context, blob *imagor.Blob, args string) (*imagor.Blob, error) {
	// Create form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add source URL - use image path if available
	sourceURL := ""
	if blob.Header != nil {
		sourceURL = blob.Header.Get("X-Source-URL")
	}
	if err := writer.WriteField("source_url", sourceURL); err != nil {
		return nil, err
	}

	// Add args if any
	if args != "" {
		if err := writer.WriteField("args", args); err != nil {
			return nil, err
		}
	}

	// Add image data
	imageData, err := blob.ReadAll()
	if err != nil {
		return nil, err
	}
	part, err := writer.CreateFormFile("image", "image.jpg")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, bytes.NewReader(imageData)); err != nil {
		return nil, err
	}

	// Close writer
	if err := writer.Close(); err != nil {
		return nil, err
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prefilter API returned status %d", resp.StatusCode)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Create result blob with preserved source URL
	resultBlob := imagor.NewBlobFromBytes(data)
	if sourceURL != "" {
		if resultBlob.Header == nil {
			resultBlob.Header = make(http.Header)
		}
		resultBlob.Header.Set("X-Source-URL", sourceURL)
	}

	return resultBlob, nil
}

// NewDepthmapPrefilter creates a new depthmap prefilter
func NewDepthmapPrefilter(apiURL string, timeout time.Duration) *HTTPPrefilter {
	return &HTTPPrefilter{
		name:    "depthmap",
		apiURL:  apiURL,
		client:  &http.Client{Timeout: timeout},
		timeout: timeout,
	}
}

// NewRemoveBGPrefilter creates a new removebg prefilter
func NewRemoveBGPrefilter(apiURL string, timeout time.Duration) *HTTPPrefilter {
	return &HTTPPrefilter{
		name:    "removebg",
		apiURL:  apiURL,
		client:  &http.Client{Timeout: timeout},
		timeout: timeout,
	}
}
