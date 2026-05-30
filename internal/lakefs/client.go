// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package lakefs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	endpoint        string
	accessKeyID     string
	secretAccessKey string
	httpClient      *http.Client
}

type RepositorySpec struct {
	Name             string
	StorageNamespace string
	DefaultBranch    string
	SampleData       bool
}

type GCRules struct {
	DefaultRetentionDays int32    `json:"default_retention_days"`
	Branches             []GCRule `json:"branches"`
}

type GCRule struct {
	BranchID      string `json:"branch_id"`
	RetentionDays int32  `json:"retention_days"`
}

func NewClient(endpoint, accessKeyID, secretAccessKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		endpoint:        strings.TrimRight(endpoint, "/"),
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		httpClient:      httpClient,
	}
}

func (c *Client) EnsureRepository(ctx context.Context, repository RepositorySpec) error {
	exists, err := c.repositoryExists(ctx, repository.Name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.createRepository(ctx, repository)
}

func (c *Client) SetGCRules(ctx context.Context, repository string, rules GCRules) error {
	body, err := json.Marshal(rules)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.repositoryURL(repository)+"/settings/gc_rules", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	default:
		return responseError("lakeFS GC rules update failed", resp)
	}
}

func (c *Client) DeleteRepository(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.repositoryURL(name)+"?force=true", nil)
	if err != nil {
		return err
	}
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return responseError("lakeFS repository delete failed", resp)
	}
}

func (c *Client) repositoryExists(ctx context.Context, name string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.repositoryURL(name), nil)
	if err != nil {
		return false, err
	}
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, responseError("lakeFS repository lookup failed", resp)
	}
}

func (c *Client) createRepository(ctx context.Context, repository RepositorySpec) error {
	body, err := json.Marshal(map[string]any{
		"name":              repository.Name,
		"storage_namespace": repository.StorageNamespace,
		"default_branch":    repository.DefaultBranch,
		"sample_data":       repository.SampleData,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.repositoriesURL(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusConflict:
		return nil
	default:
		return responseError("lakeFS repository create failed", resp)
	}
}

func responseError(message string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("%s with HTTP %d", message, resp.StatusCode)
	}
	return fmt.Errorf("%s with HTTP %d: %s", message, resp.StatusCode, detail)
}

func (c *Client) authorize(req *http.Request) {
	req.SetBasicAuth(c.accessKeyID, c.secretAccessKey)
}

func (c *Client) repositoryURL(name string) string {
	return c.endpoint + "/api/v1/repositories/" + url.PathEscape(name)
}

func (c *Client) repositoriesURL() string {
	return c.endpoint + "/api/v1/repositories?bare=false"
}
