// Package canvas provides a minimal Canvas LMS API client used by the publish command.
package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Client is a lightweight Canvas API client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Page is a Canvas page resource subset.
type Page struct {
	PageID int    `json:"page_id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

// Module is a Canvas module resource subset.
type Module struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ModuleItem is a Canvas module item resource subset.
type ModuleItem struct {
	Type    string `json:"type"`
	PageURL string `json:"page_url"`
}

// NewClient constructs a Canvas API client.
func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimSpace(baseURL)
	token = strings.TrimSpace(token)
	if baseURL == "" {
		return nil, fmt.Errorf("canvas base URL must not be empty")
	}
	if token == "" {
		return nil, fmt.Errorf("canvas API token must not be empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("canvas base URL must include scheme and host")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, httpClient: httpClient}, nil
}

// UpsertPage creates or updates a course page by title.
func (c *Client) UpsertPage(ctx context.Context, courseID, title, body string, published bool) (*Page, error) {
	existing, found, err := c.findPageByTitle(ctx, courseID, title)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("wiki_page[title]", title)
	form.Set("wiki_page[body]", body)
	form.Set("wiki_page[published]", strconv.FormatBool(published))

	var out Page
	if found {
		path := fmt.Sprintf("/api/v1/courses/%s/pages/%d", url.PathEscape(courseID), existing.PageID)
		if err := c.requestJSON(ctx, http.MethodPut, path, nil, form, &out, http.StatusOK); err != nil {
			return nil, fmt.Errorf("update page %q: %w", title, err)
		}
		return &out, nil
	}

	path := fmt.Sprintf("/api/v1/courses/%s/pages", url.PathEscape(courseID))
	if err := c.requestJSON(ctx, http.MethodPost, path, nil, form, &out, http.StatusCreated, http.StatusOK); err != nil {
		return nil, fmt.Errorf("create page %q: %w", title, err)
	}
	return &out, nil
}

// EnsureModule finds or creates a module by name.
func (c *Client) EnsureModule(ctx context.Context, courseID, moduleName string, published bool) (*Module, error) {
	existing, found, err := c.findModuleByName(ctx, courseID, moduleName)
	if err != nil {
		return nil, err
	}
	if found {
		return existing, nil
	}

	form := url.Values{}
	form.Set("module[name]", moduleName)
	form.Set("module[published]", strconv.FormatBool(published))

	var out Module
	path := fmt.Sprintf("/api/v1/courses/%s/modules", url.PathEscape(courseID))
	if err := c.requestJSON(ctx, http.MethodPost, path, nil, form, &out, http.StatusCreated, http.StatusOK); err != nil {
		return nil, fmt.Errorf("create module %q: %w", moduleName, err)
	}
	return &out, nil
}

// EnsureModulePageItem adds a page to a module when it is not already present.
func (c *Client) EnsureModulePageItem(ctx context.Context, courseID string, moduleID int, pageURL string, published bool) error {
	path := fmt.Sprintf("/api/v1/courses/%s/modules/%d/items", url.PathEscape(courseID), moduleID)
	items, err := c.listModuleItems(ctx, path)
	if err != nil {
		return fmt.Errorf("list module items: %w", err)
	}
	for _, item := range items {
		if strings.EqualFold(item.Type, "Page") && item.PageURL == pageURL {
			return nil
		}
	}

	form := url.Values{}
	form.Set("module_item[type]", "Page")
	form.Set("module_item[page_url]", pageURL)
	form.Set("module_item[published]", strconv.FormatBool(published))
	if err := c.requestJSON(ctx, http.MethodPost, path, nil, form, nil, http.StatusCreated, http.StatusOK); err != nil {
		return fmt.Errorf("create module item for page %q: %w", pageURL, err)
	}
	return nil
}

func (c *Client) listModuleItems(ctx context.Context, path string) ([]ModuleItem, error) {
	const perPage = 100

	items := make([]ModuleItem, 0, perPage)
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("per_page", strconv.Itoa(perPage))
		q.Set("page", strconv.Itoa(page))

		var batch []ModuleItem
		if err := c.requestJSON(ctx, http.MethodGet, path, q, nil, &batch, http.StatusOK); err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(batch) < perPage {
			break
		}
	}

	return items, nil
}

func (c *Client) findPageByTitle(ctx context.Context, courseID, title string) (*Page, bool, error) {
	path := fmt.Sprintf("/api/v1/courses/%s/pages", url.PathEscape(courseID))
	q := url.Values{}
	q.Set("search_term", title)
	q.Set("per_page", "100")

	var pages []Page
	if err := c.requestJSON(ctx, http.MethodGet, path, q, nil, &pages, http.StatusOK); err != nil {
		return nil, false, fmt.Errorf("search page %q: %w", title, err)
	}
	for _, p := range pages {
		if strings.EqualFold(p.Title, title) {
			return &p, true, nil
		}
	}
	return &Page{}, false, nil
}

func (c *Client) findModuleByName(ctx context.Context, courseID, moduleName string) (*Module, bool, error) {
	path := fmt.Sprintf("/api/v1/courses/%s/modules", url.PathEscape(courseID))
	q := url.Values{}
	q.Set("search_term", moduleName)
	q.Set("per_page", "100")

	var modules []Module
	if err := c.requestJSON(ctx, http.MethodGet, path, q, nil, &modules, http.StatusOK); err != nil {
		return nil, false, fmt.Errorf("search module %q: %w", moduleName, err)
	}
	for _, m := range modules {
		if strings.EqualFold(m.Name, moduleName) {
			return &m, true, nil
		}
	}
	return &Module{}, false, nil
}

func (c *Client) requestJSON(ctx context.Context, method, path string, query, form url.Values, out any, expectedStatus ...int) error {
	requestURL := c.baseURL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	var body io.Reader
	if len(form) > 0 {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if len(form) > 0 {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // Canvas base URL is validated in NewClient and explicitly configured by the operator.
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if !hasStatus(expectedStatus, resp.StatusCode) {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected HTTP status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func hasStatus(expected []int, got int) bool {
	return slices.Contains(expected, got)
}
