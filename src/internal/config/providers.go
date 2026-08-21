package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pavelveter/hermem/pkg/spi"
)

// ProviderConfig projects legacy flat settings into an application-owned,
// provider-scoped SPI configuration without exposing Config to providers.
func (c *Config) ProviderConfig(kind spi.ProviderKind) spi.ProviderConfig {
	name := ""
	var values any
	switch kind {
	case spi.ProviderKindEmbedder:
		name = c.Provider
		values = struct {
			URL       string `json:"url,omitempty"`
			Key       string `json:"key,omitempty"`
			Model     string `json:"model,omitempty"`
			ModelPath string `json:"model_path,omitempty"`
		}{c.URL, c.Key, c.Model, c.ModelPath}
	case spi.ProviderKindExtractor:
		name = c.ExtractProvider
		if name == "" {
			name = c.Provider
		}
		values = struct {
			URL         string  `json:"url,omitempty"`
			Key         string  `json:"key,omitempty"`
			Model       string  `json:"model,omitempty"`
			Temperature float32 `json:"temperature,omitempty"`
		}{c.ExtractURL, c.ExtractKey, c.ExtractModel, c.ExtractTemperature}
	case spi.ProviderKindReranker:
		name = c.RerankerProvider
		values = struct {
			URL   string `json:"url,omitempty"`
			Key   string `json:"key,omitempty"`
			Model string `json:"model,omitempty"`
		}{c.RerankerURL, c.RerankerKey, c.RerankerModel}
	case spi.ProviderKindVectorStore:
		name = c.VectorBackend
		values = struct {
			Dimensions int `json:"dimensions"`
		}{c.VectorDim}
	}
	if name == "" {
		name = "default"
	}
	raw, _ := json.Marshal(values)
	return spi.ProviderConfig{Name: strings.ToLower(strings.TrimSpace(name)), Raw: raw}
}

// ValidateProviderConfig rejects malformed provider-owned configuration at
// composition time rather than allowing a provider fallback to hide errors.
func ValidateProviderConfig(kind spi.ProviderKind, cfg spi.ProviderConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("%s provider name is required", kind)
	}
	if len(cfg.Raw) > 0 && !json.Valid(cfg.Raw) {
		return fmt.Errorf("%s provider config is invalid JSON", kind)
	}
	return nil
}
