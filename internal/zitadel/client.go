package zitadel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Project represents a Zitadel project.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Role represents a Zitadel project role.
type Role struct {
	Key         string `json:"key"`
	DisplayName string `json:"displayName"`
}

// Client talks to the Zitadel Management API (read-only).
type Client interface {
	// GetProjectByName looks up a project by name. Returns nil if not found.
	GetProjectByName(ctx context.Context, name string) (*Project, error)

	// ListProjectRoles returns all roles for a project.
	ListProjectRoles(ctx context.Context, projectID string) ([]Role, error)
}

// NewClient creates a Zitadel Management API client using a Personal Access Token.
func NewClient(baseURL, token string) Client {
	return &httpClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{},
	}
}

type httpClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func (c *httpClient) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("zitadel API %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *httpClient) GetProjectByName(ctx context.Context, name string) (*Project, error) {
	body := map[string]any{
		"queries": []map[string]any{
			{
				"nameQuery": map[string]any{
					"name":   name,
					"method": "TEXT_QUERY_METHOD_EQUALS",
				},
			},
		},
	}

	respBody, err := c.do(ctx, http.MethodPost, "/management/v1/projects/_search", body)
	if err != nil {
		return nil, fmt.Errorf("search projects: %w", err)
	}

	var result struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal project search: %w", err)
	}

	if len(result.Result) == 0 {
		return nil, nil
	}

	return &Project{
		ID:   result.Result[0].ID,
		Name: result.Result[0].Name,
	}, nil
}

func (c *httpClient) ListProjectRoles(ctx context.Context, projectID string) ([]Role, error) {
	path := fmt.Sprintf("/management/v1/projects/%s/roles/_search", projectID)
	respBody, err := c.do(ctx, http.MethodPost, path, map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("search roles: %w", err)
	}

	var result struct {
		Result []struct {
			Key         string `json:"key"`
			DisplayName string `json:"displayName"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal role search: %w", err)
	}

	roles := make([]Role, len(result.Result))
	for i, r := range result.Result {
		roles[i] = Role{Key: r.Key, DisplayName: r.DisplayName}
	}
	return roles, nil
}
