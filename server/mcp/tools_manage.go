package mcp

import (
	"context"

	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func registerManageTools(s *mcpserver.MCPServer) {
	// fs_mkdir
	s.AddTool(mcp.NewTool("fs_mkdir",
		mcp.WithDescription("Create a new directory"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Full path of directory to create")),
	), toolHandlerWithAuth(handleFsMkdir))

	// fs_rename
	s.AddTool(mcp.NewTool("fs_rename",
		mcp.WithDescription("Rename a file or directory"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Current path of the file/directory")),
		mcp.WithString("name", mcp.Required(), mcp.Description("New name (filename only, not a path)")),
	), toolHandlerWithAuth(handleFsRename))

	// fs_move
	s.AddTool(mcp.NewTool("fs_move",
		mcp.WithDescription("Move files/directories to another location"),
		mcp.WithString("src_dir", mcp.Required(), mcp.Description("Source directory")),
		mcp.WithString("dst_dir", mcp.Required(), mcp.Description("Destination directory")),
		mcp.WithArray("names", mcp.Description("Names of files/directories to move")),
	), toolHandlerWithAuth(handleFsMove))

	// fs_copy
	s.AddTool(mcp.NewTool("fs_copy",
		mcp.WithDescription("Copy files/directories to another location"),
		mcp.WithString("src_dir", mcp.Required(), mcp.Description("Source directory")),
		mcp.WithString("dst_dir", mcp.Required(), mcp.Description("Destination directory")),
		mcp.WithArray("names", mcp.Description("Names of files/directories to copy")),
	), toolHandlerWithAuth(handleFsCopy))

	// fs_remove
	s.AddTool(mcp.NewTool("fs_remove",
		mcp.WithDescription("Delete files/directories"),
		mcp.WithString("dir", mcp.Required(), mcp.Description("Directory containing items to remove")),
		mcp.WithArray("names", mcp.Description("Names of files/directories to remove")),
	), toolHandlerWithAuth(handleFsRemove))
}

func handleFsMkdir(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathStr, err := req.RequireString("path")
	if err != nil {
		return toolError("path is required")
	}

	reqPath, err := user.JoinPath(pathStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkManage(user, reqPath, common.PermWrite); err != nil {
		return toolError(err.Error())
	}

	ctx = context.WithValue(ctx, "user", user)
	if err := fs.MakeDir(ctx, reqPath); err != nil {
		return wrapError(err)
	}
	return textResult("directory created successfully")
}

func handleFsRename(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathStr, err := req.RequireString("path")
	if err != nil {
		return toolError("path is required")
	}
	name, err := req.RequireString("name")
	if err != nil {
		return toolError("name is required")
	}
	if err := utils.ValidateNameComponent(name); err != nil {
		return toolErrorf("invalid name: %s", err.Error())
	}

	reqPath, err := user.JoinPath(pathStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkManage(user, reqPath, common.PermRename); err != nil {
		return toolError(err.Error())
	}

	ctx = context.WithValue(ctx, "user", user)
	if err := fs.Rename(ctx, reqPath, name); err != nil {
		return wrapError(err)
	}
	return textResult("renamed successfully")
}

func handleFsMove(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	srcDirStr, err := req.RequireString("src_dir")
	if err != nil {
		return toolError("src_dir is required")
	}
	dstDirStr, err := req.RequireString("dst_dir")
	if err != nil {
		return toolError("dst_dir is required")
	}
	names := getStringArray(req, "names")
	if len(names) == 0 {
		return toolError("names is required and must not be empty")
	}

	srcDir, err := user.JoinPath(srcDirStr)
	if err != nil {
		return wrapError(err)
	}
	dstDir, err := user.JoinPath(dstDirStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkManage(user, srcDir, common.PermMove); err != nil {
		return toolError(err.Error())
	}

	ctx = context.WithValue(ctx, "user", user)
	for i, name := range names {
		srcPath, err := utils.JoinUnderBase(srcDir, name)
		if err != nil {
			return toolErrorf("invalid name %q: %s", name, err.Error())
		}
		if err := fs.Move(ctx, srcPath, dstDir, len(names) > i+1); err != nil {
			return wrapError(err)
		}
	}
	return textResult("moved successfully")
}

func handleFsCopy(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	srcDirStr, err := req.RequireString("src_dir")
	if err != nil {
		return toolError("src_dir is required")
	}
	dstDirStr, err := req.RequireString("dst_dir")
	if err != nil {
		return toolError("dst_dir is required")
	}
	names := getStringArray(req, "names")
	if len(names) == 0 {
		return toolError("names is required and must not be empty")
	}

	srcDir, err := user.JoinPath(srcDirStr)
	if err != nil {
		return wrapError(err)
	}
	dstDir, err := user.JoinPath(dstDirStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkManage(user, srcDir, common.PermCopy); err != nil {
		return toolError(err.Error())
	}

	ctx = context.WithValue(ctx, "user", user)
	for i, name := range names {
		srcPath, err := utils.JoinUnderBase(srcDir, name)
		if err != nil {
			return toolErrorf("invalid name %q: %s", name, err.Error())
		}
		if _, err := fs.Copy(ctx, srcPath, dstDir, len(names) > i+1); err != nil {
			return wrapError(err)
		}
	}
	return textResult("copied successfully")
}

func handleFsRemove(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dirStr, err := req.RequireString("dir")
	if err != nil {
		return toolError("dir is required")
	}
	names := getStringArray(req, "names")
	if len(names) == 0 {
		return toolError("names is required and must not be empty")
	}

	reqDir, err := user.JoinPath(dirStr)
	if err != nil {
		return wrapError(err)
	}
	if err := checkManage(user, reqDir, common.PermRemove); err != nil {
		return toolError(err.Error())
	}

	ctx = context.WithValue(ctx, "user", user)
	for _, name := range names {
		removePath, err := utils.JoinUnderBase(reqDir, name)
		if err != nil {
			return toolErrorf("invalid name %q: %s", name, err.Error())
		}
		if err := fs.Remove(ctx, removePath); err != nil {
			return wrapError(err)
		}
	}
	return textResult("removed successfully")
}

// getStringArray extracts a string array from tool request arguments.
func getStringArray(req mcp.CallToolRequest, name string) []string {
	args := req.GetArguments()
	val, ok := args[name]
	if !ok {
		return nil
	}
	arr, ok := val.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
