package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	stdpath "path"
	"strconv"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func registerUploadTools(s *mcpserver.MCPServer) {
	s.AddTool(mcp.NewTool("fs_upload",
		mcp.WithDescription("Upload a local file to alist. Automatically uses direct internal upload (local deployment) or HTTP API upload (remote deployment)."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Destination path in alist including filename")),
		mcp.WithString("local_path", mcp.Required(), mcp.Description("Absolute local file path to upload")),
	), toolHandlerWithAuth(handleFsUpload))
}

func handleFsUpload(ctx context.Context, user *model.User, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	pathStr, err := req.RequireString("path")
	if err != nil {
		return toolError("path is required")
	}
	localPath, err := req.RequireString("local_path")
	if err != nil {
		return toolError("local_path is required")
	}

	reqPath, err := user.JoinPath(pathStr)
	if err != nil {
		return wrapError(err)
	}
	dir := stdpath.Dir(reqPath)

	if err := checkManage(user, dir, common.PermWrite); err != nil {
		return toolError(err.Error())
	}

	if strings.Contains(conf.Conf.SiteURL, "://") {
		return uploadViaHTTP(reqPath, localPath)
	}
	return uploadDirectly(ctx, user, reqPath, localPath)
}

func uploadDirectly(ctx context.Context, user *model.User, reqPath, localPath string) (*mcp.CallToolResult, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return toolErrorf("failed to open local file: %s", err.Error())
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return toolErrorf("failed to stat local file: %s", err.Error())
	}

	dir := stdpath.Dir(reqPath)
	name := stdpath.Base(reqPath)

	fileStream := &stream.FileStream{
		Ctx: ctx,
		Obj: &model.Object{
			Name:     name,
			Size:     info.Size(),
			Modified: time.Now(),
			IsFolder: false,
		},
		Reader:  io.NopCloser(file),
		Closers: utils.EmptyClosers(),
	}

	ctx = context.WithValue(ctx, "user", user)
	if err := fs.PutDirectly(ctx, dir, fileStream); err != nil {
		return wrapError(err)
	}
	return textResult("uploaded successfully")
}

func uploadViaHTTP(reqPath, localPath string) (*mcp.CallToolResult, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return toolErrorf("failed to open local file: %s", err.Error())
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return toolErrorf("failed to stat local file: %s", err.Error())
	}

	name := stdpath.Base(reqPath)
	apiURL := fmt.Sprintf("%s/api/fs/put", conf.Conf.SiteURL)

	httpReq, err := http.NewRequest(http.MethodPut, apiURL, file)
	if err != nil {
		return toolErrorf("failed to create request: %s", err.Error())
	}

	httpReq.Header.Set("File-Path", url.PathEscape(reqPath))
	httpReq.Header.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	httpReq.Header.Set("Content-Type", utils.GetMimeType(name))
	httpReq.Header.Set("Authorization", setting.GetStr(conf.Token))
	httpReq.ContentLength = info.Size()

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return toolErrorf("upload request failed: %s", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return toolErrorf("upload failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return textResult("uploaded successfully")
}
