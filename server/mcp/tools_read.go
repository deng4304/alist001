package mcp

import (
	"context"
	"path"
	"strings"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/search"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/pkg/errors"
)

func registerReadTools(s *mcpserver.MCPServer) {
	// fs_list
	s.AddTool(mcp.NewTool("fs_list",
		mcp.WithDescription("List files and directories at the given path"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Directory path to list")),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("per_page", mcp.Description("Items per page (default: 30, max: 500)")),
		mcp.WithBoolean("refresh", mcp.Description("Force refresh from storage (default: false)")),
	), toolHandlerWithAuth(handleFsList))

	// fs_get
	s.AddTool(mcp.NewTool("fs_get",
		mcp.WithDescription("Get file or directory metadata"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to file or directory")),
	), toolHandlerWithAuth(handleFsGet))

	// fs_search
	s.AddTool(mcp.NewTool("fs_search",
		mcp.WithDescription("Search for files by keywords"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Parent directory to search within")),
		mcp.WithString("keywords", mcp.Required(), mcp.Description("Search keywords")),
		mcp.WithNumber("scope", mcp.Description("0=all, 1=dir only, 2=file only (default: 0)")),
		mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
		mcp.WithNumber("per_page", mcp.Description("Items per page (default: 20)")),
	), toolHandlerWithAuth(handleFsSearch))

	// fs_download_url
	s.AddTool(mcp.NewTool("fs_download_url",
		mcp.WithDescription("Get download URL for a file"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the file")),
	), toolHandlerWithAuth(handleFsDownloadURL))
}

func handleFsList(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathStr, err := req.RequireString("path")
	if err != nil {
		return toolError("path is required")
	}
	page := intParam(req, "page", 1)
	perPage := intParam(req, "per_page", 30)
	if perPage > 500 {
		perPage = 500
	}
	refresh := req.GetBool("refresh", false)

	ctx, reqPath, err := buildFsContext(ctx, user, pathStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkAccess(user, reqPath); err != nil {
		return toolError(err.Error())
	}

	objs, err := fs.List(ctx, reqPath, &fs.ListArgs{Refresh: refresh})
	if err != nil {
		return wrapError(err)
	}

	// Paginate
	total := len(objs)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	pageObjs := objs[start:end]

	items := make([]objJSON, 0, len(pageObjs))
	for _, obj := range pageObjs {
		items = append(items, objToJSON(obj))
	}

	return jsonResult(map[string]interface{}{
		"content": items,
		"total":   total,
		"page":    page,
		"per_page": perPage,
	})
}

func handleFsGet(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathStr, err := req.RequireString("path")
	if err != nil {
		return toolError("path is required")
	}

	ctx, reqPath, err := buildFsContext(ctx, user, pathStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkAccess(user, reqPath); err != nil {
		return toolError(err.Error())
	}

	obj, err := fs.Get(ctx, reqPath, &fs.GetArgs{})
	if err != nil {
		return wrapError(err)
	}

	return jsonResult(objToJSON(obj))
}

func handleFsSearch(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathStr, err := req.RequireString("path")
	if err != nil {
		return toolError("path is required")
	}
	keywords, err := req.RequireString("keywords")
	if err != nil {
		return toolError("keywords is required")
	}
	scope := intParam(req, "scope", 0)
	page := intParam(req, "page", 1)
	perPage := intParam(req, "per_page", 20)

	parent, err := user.JoinPath(pathStr)
	if err != nil {
		return wrapError(err)
	}

	searchReq := model.SearchReq{
		Parent:   parent,
		Keywords: keywords,
		Scope:    scope,
		PageReq:  model.PageReq{Page: page, PerPage: perPage},
	}
	if err := searchReq.Validate(); err != nil {
		return toolErrorf("invalid search request: %s", err.Error())
	}

	nodes, total, err := search.Search(ctx, searchReq)
	if err != nil {
		return wrapError(err)
	}

	// Filter by permission
	filtered := make([]model.SearchNode, 0, len(nodes))
	for _, node := range nodes {
		if !strings.HasPrefix(node.Parent, user.BasePath) {
			continue
		}
		meta, err := op.GetNearestMeta(node.Parent)
		if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			continue
		}
		if !common.CanAccessWithRoles(user, meta, path.Join(node.Parent, node.Name), "") {
			continue
		}
		filtered = append(filtered, node)
	}

	return jsonResult(map[string]interface{}{
		"content": filtered,
		"total":   total,
	})
}

func handleFsDownloadURL(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathStr, err := req.RequireString("path")
	if err != nil {
		return toolError("path is required")
	}

	ctx, reqPath, err := buildFsContext(ctx, user, pathStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkAccess(user, reqPath); err != nil {
		return toolError(err.Error())
	}

	link, _, err := fs.Link(ctx, reqPath, model.LinkArgs{})
	if err != nil {
		return wrapError(err)
	}

	return jsonResult(map[string]interface{}{
		"raw_url": link.URL,
	})
}

// intParam extracts an integer parameter with a default value.
func intParam(req mcp.CallToolRequest, name string, defaultVal int) int {
	v := req.GetFloat(name, float64(defaultVal))
	return int(v)
}
