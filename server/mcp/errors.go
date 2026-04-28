package mcp

import (
	"fmt"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/mark3labs/mcp-go/mcp"
	pkgerr "github.com/pkg/errors"
)

func toolError(msg string) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(msg), nil
}

func toolErrorf(format string, args ...interface{}) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultError(fmt.Sprintf(format, args...)), nil
}

func wrapError(err error) (*mcp.CallToolResult, error) {
	if err == nil {
		return nil, nil
	}
	cause := pkgerr.Cause(err)
	switch {
	case errs.IsObjectNotFound(err) || errs.IsNotFoundError(err):
		return toolErrorf("not found: %s", err.Error())
	case cause == errs.PermissionDenied:
		return toolError("permission denied")
	case cause == errs.NotImplement:
		return toolError("not supported by storage driver")
	case cause == errs.NotSupport:
		return toolError("operation not supported")
	case cause == errs.UploadNotSupported:
		return toolError("upload not supported by storage")
	case cause == errs.MoveBetweenTwoStorages:
		return toolError("can't move between two storages, use copy instead")
	default:
		return toolErrorf("error: %s", err.Error())
	}
}
