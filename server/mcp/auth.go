package mcp

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/server/common"
	log "github.com/sirupsen/logrus"
)

type ctxKey string

const userKey ctxKey = "user"

// HTTPContextFunc extracts JWT/admin token from HTTP request and injects user into context.
// Used as WithHTTPContextFunc callback for Streamable HTTP transport.
func HTTPContextFunc(ctx context.Context, r *http.Request) context.Context {
	token := r.Header.Get("Authorization")
	if token == "" {
		token = r.URL.Query().Get("token")
	}

	user, err := authenticateToken(token)
	if err != nil {
		log.Debugf("MCP auth failed: %v", err)
		return ctx
	}

	return context.WithValue(ctx, userKey, user)
}

func authenticateToken(token string) (*model.User, error) {
	// Check admin static token
	if token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(setting.GetStr(conf.Token))) == 1 {
		admin, err := op.GetAdmin()
		if err != nil {
			return nil, fmt.Errorf("failed to get admin: %w", err)
		}
		if err := loadRoles(admin); err != nil {
			return nil, err
		}
		return admin, nil
	}

	// No token: guest
	if token == "" {
		guest, err := op.GetGuest()
		if err != nil {
			return nil, fmt.Errorf("failed to get guest: %w", err)
		}
		if guest.Disabled {
			return nil, fmt.Errorf("guest user is disabled")
		}
		if err := loadRoles(guest); err != nil {
			return nil, err
		}
		return guest, nil
	}

	// JWT token
	claims, err := common.ParseToken(token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	user, err := op.GetUserByName(claims.Username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	if claims.PwdTS != user.PwdTS {
		return nil, fmt.Errorf("password has been changed")
	}
	if user.Disabled {
		return nil, fmt.Errorf("user is disabled")
	}

	if err := loadRoles(user); err != nil {
		return nil, err
	}
	return user, nil
}

func loadRoles(user *model.User) error {
	if len(user.Role) > 0 {
		roles, err := op.GetRolesByUserID(user.ID)
		if err != nil {
			return fmt.Errorf("failed to load roles: %w", err)
		}
		user.RolesDetail = roles
	}
	return nil
}

// resolveUser extracts the authenticated user from context.
func resolveUser(ctx context.Context) (*model.User, error) {
	user, ok := ctx.Value(userKey).(*model.User)
	if !ok || user == nil {
		return nil, fmt.Errorf("authentication required")
	}
	return user, nil
}

// buildFsContext resolves path and sets meta in context for fs operations.
func buildFsContext(ctx context.Context, user *model.User, path string) (context.Context, string, error) {
	reqPath, err := user.JoinPath(path)
	if err != nil {
		return ctx, "", err
	}
	meta, _ := op.GetNearestMeta(reqPath)
	ctx = context.WithValue(ctx, "meta", meta)
	ctx = context.WithValue(ctx, "user", user)
	return ctx, reqPath, nil
}

// checkAccess checks if user can access the path (read).
func checkAccess(user *model.User, reqPath string) error {
	meta, _ := op.GetNearestMeta(reqPath)
	if !common.CanAccessWithRoles(user, meta, reqPath, "") {
		return fmt.Errorf("permission denied")
	}
	perm := common.MergeRolePermissions(user, reqPath)
	if !user.IsAdmin() && !common.HasPermission(perm, common.PermMCPAccess) {
		return fmt.Errorf("MCP access not permitted")
	}
	return nil
}

// checkManage checks if user can perform write operations via MCP.
func checkManage(user *model.User, reqPath string, permBit uint) error {
	if err := checkAccess(user, reqPath); err != nil {
		return err
	}
	perm := common.MergeRolePermissions(user, reqPath)
	if !user.IsAdmin() && !common.HasPermission(perm, common.PermMCPManage) {
		return fmt.Errorf("MCP manage not permitted")
	}
	if !user.IsAdmin() && !common.HasPermission(perm, permBit) {
		return fmt.Errorf("permission denied for this operation")
	}
	return nil
}

// UserContextFunc returns an HTTPContextFunc that injects a specific user (for STDIO mode).
func userContextMiddleware(user *model.User) func(ctx context.Context) context.Context {
	return func(ctx context.Context) context.Context {
		return context.WithValue(ctx, userKey, user)
	}
}

// resolveUserForStdio resolves a user by username for STDIO mode.
func resolveUserForStdio(username string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || username == "admin" {
		admin, err := op.GetAdmin()
		if err != nil {
			return nil, fmt.Errorf("failed to get admin user: %w", err)
		}
		if err := loadRoles(admin); err != nil {
			return nil, err
		}
		return admin, nil
	}
	user, err := op.GetUserByName(username)
	if err != nil {
		return nil, fmt.Errorf("user %q not found: %w", username, err)
	}
	if user.Disabled {
		return nil, fmt.Errorf("user %q is disabled", username)
	}
	if err := loadRoles(user); err != nil {
		return nil, err
	}
	return user, nil
}
