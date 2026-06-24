package ai

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"
)

// retryDo wraps client.Do with exponential backoff on 5xx/429/network errors.
// The request must have GetBody set (http.NewRequest does NOT set it) so the
// body can be recreated between retries. Returns the first 2xx/4xx response
// or the last error after all attempts.
func retryDo(client *http.Client, req *http.Request, attempts int) (*http.Response, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("retry: get body: %w", err)
			}
			req.Body = body
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond)
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			buf := make([]byte, 256)
			n, _ := resp.Body.Read(buf)
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(buf[:n]))
			time.Sleep(time.Duration(i+1)*time.Second + time.Duration(rand.Intn(500))*time.Millisecond)
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// newRequestWithRetry creates an http.Request with GetBody set so retryDo can
// re-create the request body between attempts.
func newRequestWithRetry(method, url string, body []byte) (*http.Request, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	b := body
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	return req, nil
}
