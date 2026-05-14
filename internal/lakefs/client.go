// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package lakefs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		return false, fmt.Errorf("lakeFS repository lookup failed with HTTP %d", resp.StatusCode)
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
		return fmt.Errorf("lakeFS repository create failed with HTTP %d", resp.StatusCode)
	}
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
