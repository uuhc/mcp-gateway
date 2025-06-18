package handler

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/amoylab/unla/internal/apiserver/database"
	"github.com/amoylab/unla/internal/auth/jwt"
	"github.com/amoylab/unla/internal/common/config"
	"github.com/amoylab/unla/internal/common/dto"
	"github.com/amoylab/unla/internal/i18n"
	"github.com/amoylab/unla/internal/mcp/storage"
	"github.com/amoylab/unla/internal/mcp/storage/notifier"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type MCP struct {
	db       database.Database
	store    storage.Store
	notifier notifier.Notifier
	logger   *zap.Logger
}

func NewMCP(db database.Database, store storage.Store, ntf notifier.Notifier, logger *zap.Logger) *MCP {
	return &MCP{
		db:       db,
		store:    store,
		notifier: ntf,
		logger:   logger,
	}
}

// checkTenantPermission checks if the user has permission to access the specified tenant and
// verifies that all router prefixes start with the tenant prefix as a complete path segment
func (h *MCP) checkTenantPermission(c *gin.Context, tenantName string, cfg *config.MCPConfig) (*database.Tenant, error) {
	h.logger.Debug("checking tenant permission",
		zap.String("tenant_name", tenantName))

	// Check if tenant name is empty
	if tenantName == "" {
		h.logger.Warn("tenant name is empty")
		return nil, i18n.ErrorTenantNameRequired
	}

	// Get user authentication information
	claims, exists := c.Get("claims")
	if !exists {
		h.logger.Warn("missing JWT claims in context")
		return nil, i18n.ErrUnauthorized
	}
	jwtClaims := claims.(*jwt.Claims)

	// Get user information
	user, err := h.db.GetUserByUsername(c.Request.Context(), jwtClaims.Username)
	if err != nil {
		h.logger.Error("failed to get user info",
			zap.String("username", jwtClaims.Username),
			zap.Error(err))
		return nil, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user info: "+err.Error())
	}

	// Get tenant information
	tenant, err := h.db.GetTenantByName(c.Request.Context(), tenantName)
	if err != nil {
		h.logger.Warn("tenant not found",
			zap.String("tenant_name", tenantName),
			zap.Error(err))
		return nil, i18n.ErrorTenantNotFound.WithParam("Name", tenantName)
	}

	// Normalize tenant prefix
	tenantPrefix := tenant.Prefix
	if !strings.HasPrefix(tenantPrefix, "/") {
		tenantPrefix = "/" + tenantPrefix
	}
	tenantPrefix = strings.TrimSuffix(tenantPrefix, "/")

	// Check if all router prefixes start with tenant prefix
	for _, router := range cfg.Routers {
		// Normalize router prefix
		routerPrefix := router.Prefix
		if !strings.HasPrefix(routerPrefix, "/") {
			routerPrefix = "/" + routerPrefix
		}
		routerPrefix = strings.TrimSuffix(routerPrefix, "/")

		// Allow exact match
		if routerPrefix == tenantPrefix {
			continue
		}

		// Must start with tenant prefix followed by a path separator
		if !strings.HasPrefix(routerPrefix, tenantPrefix+"/") {
			h.logger.Warn("router prefix validation failed",
				zap.String("router_prefix", routerPrefix),
				zap.String("tenant_prefix", tenantPrefix))
			return nil, i18n.ErrorRouterPrefixError
		}
	}

	// Check user permission if not admin
	if user.Role != database.RoleAdmin {
		userTenants, err := h.db.GetUserTenants(c.Request.Context(), user.ID)
		if err != nil {
			h.logger.Error("failed to get user tenants",
				zap.Uint("user_id", user.ID),
				zap.Error(err))
			return nil, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user tenants: "+err.Error())
		}

		allowed := false
		for _, userTenant := range userTenants {
			if userTenant.ID == tenant.ID {
				allowed = true
				break
			}
		}

		if !allowed {
			h.logger.Warn("user lacks permission for tenant",
				zap.Uint("user_id", user.ID),
				zap.Uint("tenant_id", tenant.ID))
			return nil, i18n.ErrorTenantPermissionError
		}
	}

	h.logger.Debug("tenant permission check passed",
		zap.String("tenant_name", tenantName),
		zap.Uint("tenant_id", tenant.ID))
	return tenant, nil
}

func (h *MCP) HandleMCPServerUpdate(c *gin.Context) {
	// Read the raw YAML content from request body
	content, err := c.GetRawData()
	if err != nil {
		h.logger.Error("failed to read request body", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrBadRequest.WithParam("Reason", "Failed to read request body: "+err.Error()))
		return
	}

	// Validate the YAML content
	var cfg config.MCPConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		h.logger.Error("invalid YAML content", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Invalid YAML content: "+err.Error()))
		return
	}

	// Get existing server
	oldCfg, err := h.store.Get(c.Request.Context(), cfg.Tenant, cfg.Name)
	if err != nil {
		h.logger.Error("MCP server not found",
			zap.String("server_name", cfg.Name),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrorMCPServerNotFound.WithParam("Name", cfg.Name))
		return
	}

	if oldCfg.Name != cfg.Name {
		h.logger.Warn("server name mismatch",
			zap.String("config_name", cfg.Name))
		i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Server name in configuration must match name parameter"))
		return
	}

	_, err = h.checkTenantPermission(c, cfg.Tenant, &cfg)
	if err != nil {
		h.logger.Warn("tenant permission check failed",
			zap.String("tenant", cfg.Tenant),
			zap.Error(err))
		i18n.RespondWithError(c, err)
		return
	}

	// Get all existing configurations
	configs, err := h.store.List(c.Request.Context())
	if err != nil {
		h.logger.Error("failed to get existing configurations", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get existing configurations: "+err.Error()))
		return
	}

	// Merge the new configuration with existing configs
	configs = config.MergeConfigs(configs, &cfg)

	// Validate all configurations
	if err := config.ValidateMCPConfigs(configs); err != nil {
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			h.logger.Error("configuration validation failed",
				zap.String("validation_error", validationErr.Error()))
			i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Configuration validation failed: "+validationErr.Error()))
		} else {
			h.logger.Error("failed to validate configurations", zap.Error(err))
			i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Failed to validate configurations: "+err.Error()))
		}
		return
	}

	if err := h.store.Update(c.Request.Context(), &cfg); err != nil {
		h.logger.Error("failed to update MCP server",
			zap.String("server_name", cfg.Name),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to update MCP server: "+err.Error()))
		return
	}

	// Send reload signal to gateway using notifier
	if err := h.notifier.NotifyUpdate(c.Request.Context(), &cfg); err != nil {
		h.logger.Error("failed to reload gateway", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to reload gateway: "+err.Error()))
		return
	}

	h.logger.Info("MCP server updated successfully",
		zap.String("server_name", cfg.Name))
	i18n.Success(i18n.SuccessMCPServerUpdated).With("status", "success").Send(c)
}

func (h *MCP) HandleListMCPServers(c *gin.Context) {
	h.logger.Info("handling list MCP servers request")

	tenantIDStr := c.Query("tenantId")
	var tenantID uint
	if tenantIDStr != "" {
		tid, err := strconv.ParseUint(tenantIDStr, 10, 32)
		if err != nil {
			h.logger.Warn("invalid tenantId parameter",
				zap.String("tenantId", tenantIDStr))
			i18n.RespondWithError(c, i18n.ErrBadRequest.WithParam("Reason", "Invalid tenantId parameter"))
			return
		}
		tenantID = uint(tid)
		h.logger.Debug("filtering by tenant ID", zap.Uint("tenant_id", tenantID))
	}

	claims, exists := c.Get("claims")
	if !exists {
		h.logger.Warn("missing JWT claims in context")
		i18n.RespondWithError(c, i18n.ErrUnauthorized)
		return
	}
	jwtClaims := claims.(*jwt.Claims)

	user, err := h.db.GetUserByUsername(c.Request.Context(), jwtClaims.Username)
	if err != nil {
		h.logger.Error("failed to get user info",
			zap.String("username", jwtClaims.Username),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user info: "+err.Error()))
		return
	}

	servers, err := h.store.List(c.Request.Context())
	if err != nil {
		h.logger.Error("failed to get MCP servers", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get MCP servers: "+err.Error()))
		return
	}

	if user.Role != database.RoleAdmin && tenantID > 0 {
		userTenants, err := h.db.GetUserTenants(c.Request.Context(), user.ID)
		if err != nil {
			h.logger.Error("failed to get user tenants",
				zap.Uint("user_id", user.ID),
				zap.Error(err))
			i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user tenants: "+err.Error()))
			return
		}

		hasPermission := false
		for _, tenant := range userTenants {
			if tenant.ID == tenantID {
				hasPermission = true
				break
			}
		}

		if !hasPermission {
			h.logger.Warn("user lacks permission for tenant",
				zap.Uint("user_id", user.ID),
				zap.Uint("tenant_id", tenantID))
			i18n.RespondWithError(c, i18n.ErrorTenantPermissionError)
			return
		}
	}

	var filteredServers []*config.MCPConfig
	if tenantID > 0 {
		tenant, err := h.db.GetTenantByID(c.Request.Context(), tenantID)
		if err != nil {
			h.logger.Error("tenant not found",
				zap.Uint("tenant_id", tenantID),
				zap.Error(err))
			i18n.RespondWithError(c, i18n.ErrorTenantNotFound)
			return
		}

		name := tenant.Name
		for _, server := range servers {
			if server.Tenant == name {
				filteredServers = append(filteredServers, server)
			}
		}
		h.logger.Debug("filtered servers by tenant name",
			zap.String("tenant_name", name),
			zap.Int("server_count", len(filteredServers)))
	} else if user.Role != database.RoleAdmin {
		userTenants, err := h.db.GetUserTenants(c.Request.Context(), user.ID)
		if err != nil {
			h.logger.Error("failed to get user tenants",
				zap.Uint("user_id", user.ID),
				zap.Error(err))
			i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user tenants: "+err.Error()))
			return
		}

		tenantNames := make([]string, len(userTenants))
		for i, tenant := range userTenants {
			tenantNames[i] = tenant.Name
		}

		for _, server := range servers {
			for _, name := range tenantNames {
				if server.Tenant == name {
					filteredServers = append(filteredServers, server)
					break
				}
			}
		}
		h.logger.Debug("filtered servers by user tenants",
			zap.Uint("user_id", user.ID),
			zap.Int("tenant_count", len(tenantNames)),
			zap.Int("server_count", len(filteredServers)))
	} else {
		filteredServers = servers
		h.logger.Debug("admin user, returning all servers",
			zap.Int("server_count", len(servers)))
	}

	// TODO: temporary
	results := make([]*dto.MCPServer, len(filteredServers))
	for i, server := range filteredServers {
		results[i] = &dto.MCPServer{
			Name:       server.Name,
			Tenant:     server.Tenant,
			McpServers: dto.FromMCPServerConfigs(server.McpServers),
			Tools:      dto.FromToolConfigs(server.Tools),
			Servers:    dto.FromServerConfigs(server.Servers),
			Routers:    dto.FromRouterConfigs(server.Routers),
			CreatedAt:  server.CreatedAt,
			UpdatedAt:  server.UpdatedAt,
		}
	}

	h.logger.Info("returning MCP server list",
		zap.Int("server_count", len(results)))
	i18n.Success(i18n.SuccessMCPServerList).With("data", results).Send(c)
}

func (h *MCP) HandleMCPServerCreate(c *gin.Context) {
	h.logger.Info("handling MCP server create request")

	// Read the raw YAML content from request body
	content, err := c.GetRawData()
	if err != nil {
		h.logger.Error("failed to read request body", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrBadRequest.WithParam("Reason", "Failed to read request body: "+err.Error()))
		return
	}

	// Validate the YAML content and get the server name
	var cfg config.MCPConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		h.logger.Error("invalid YAML content", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Invalid YAML content: "+err.Error()))
		return
	}

	if cfg.Name == "" {
		h.logger.Warn("server name is required in configuration")
		i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Server name is required in configuration"))
		return
	}

	h.logger.Debug("validating server configuration",
		zap.String("server_name", cfg.Name),
		zap.String("tenant", cfg.Tenant))

	_, err = h.checkTenantPermission(c, cfg.Tenant, &cfg)
	if err != nil {
		h.logger.Warn("tenant permission check failed",
			zap.String("tenant", cfg.Tenant),
			zap.Error(err))

		i18n.RespondWithError(c, err)
		return
	}

	// Check if server already exists
	_, err = h.store.Get(c.Request.Context(), cfg.Tenant, cfg.Name)
	if err == nil {
		h.logger.Warn("MCP server already exists",
			zap.String("server_name", cfg.Name))
		i18n.RespondWithError(c, i18n.ErrorMCPServerExists.WithParam("Name", cfg.Name))
		return
	}

	// Get all existing configurations
	configs, err := h.store.List(c.Request.Context())
	if err != nil {
		h.logger.Error("failed to get existing configurations", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get existing configurations: "+err.Error()))
		return
	}

	// Add the new configuration to the list
	configs = append(configs, &cfg)

	// Validate all configurations
	if err := config.ValidateMCPConfigs(configs); err != nil {
		var validationErr *config.ValidationError
		if errors.As(err, &validationErr) {
			h.logger.Error("configuration validation failed",
				zap.String("validation_error", validationErr.Error()))
			i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Configuration validation failed: "+validationErr.Error()))
		} else {
			h.logger.Error("failed to validate configurations", zap.Error(err))
			i18n.RespondWithError(c, i18n.ErrorMCPServerValidation.WithParam("Reason", "Failed to validate configurations: "+err.Error()))
		}
		return
	}

	if err := h.store.Create(c.Request.Context(), &cfg); err != nil {
		h.logger.Error("failed to create MCP server",
			zap.String("server_name", cfg.Name),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to create MCP server: "+err.Error()))
		return
	}

	// Send reload signal to gateway using notifier
	if err := h.notifier.NotifyUpdate(c.Request.Context(), &cfg); err != nil {
		h.logger.Error("failed to reload gateway", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to reload gateway: "+err.Error()))
		return
	}

	h.logger.Info("MCP server created successfully",
		zap.String("server_name", cfg.Name))
	i18n.Created(i18n.SuccessMCPServerCreated).With("status", "success").Send(c)
}

func (h *MCP) HandleMCPServerDelete(c *gin.Context) {
	// Get the server name from path parameter
	tenant := c.Param("tenant")
	if tenant == "" {
		h.logger.Warn("MCP server tenant required but missing")
		i18n.RespondWithError(c, i18n.ErrorTenantRequired)
		return
	}
	name := c.Param("name")
	if name == "" {
		h.logger.Warn("MCP server name required but missing")
		i18n.RespondWithError(c, i18n.ErrorMCPServerNameRequired)
		return
	}

	h.logger.Info("handling MCP server delete request",
		zap.String("server_name", name))

	// Check if server exists
	existingCfg, err := h.store.Get(c.Request.Context(), tenant, name)
	if err != nil {
		h.logger.Error("MCP server not found",
			zap.String("server_name", name),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrorMCPServerNotFound.WithParam("Name", name))
		return
	}

	// Check tenant permission
	_, err = h.checkTenantPermission(c, existingCfg.Tenant, existingCfg)
	if err != nil {
		h.logger.Warn("tenant permission check failed",
			zap.String("tenant", existingCfg.Tenant),
			zap.Error(err))
		i18n.RespondWithError(c, err)
		return
	}

	// Delete server
	if err := h.store.Delete(c.Request.Context(), existingCfg.Tenant, name); err != nil {
		h.logger.Error("failed to delete MCP server",
			zap.String("server_name", name),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to delete MCP server: "+err.Error()))
		return
	}

	// Send reload signal to gateway using notifier
	existingCfg.DeletedAt = time.Now()
	if err := h.notifier.NotifyUpdate(c.Request.Context(), existingCfg); err != nil {
		h.logger.Error("failed to notify gateway", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to notify gateway: "+err.Error()))
		return
	}

	h.logger.Info("MCP server deleted successfully",
		zap.String("server_name", name))
	i18n.Success(i18n.SuccessMCPServerDeleted).With("status", "success").Send(c)
}

func (h *MCP) HandleMCPServerSync(c *gin.Context) {
	// Get user from claims
	claims, exists := c.Get("claims")
	if !exists {
		h.logger.Warn("missing JWT claims in context")
		i18n.RespondWithError(c, i18n.ErrUnauthorized)
		return
	}
	jwtClaims := claims.(*jwt.Claims)

	// Get user information
	user, err := h.db.GetUserByUsername(c.Request.Context(), jwtClaims.Username)
	if err != nil {
		h.logger.Error("failed to get user info",
			zap.String("username", jwtClaims.Username),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user info: "+err.Error()))
		return
	}

	// Only admin can sync all servers
	if user.Role != database.RoleAdmin {
		h.logger.Warn("non-admin user attempted to sync all servers",
			zap.String("username", user.Username))
		i18n.RespondWithError(c, i18n.ErrUnauthorized)
		return
	}

	h.logger.Info("handling MCP server sync request")

	// Send reload signal to gateway using notifier
	if err := h.notifier.NotifyUpdate(c.Request.Context(), nil); err != nil {
		h.logger.Error("failed to reload gateway", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to reload gateway: "+err.Error()))
		return
	}

	h.logger.Info("MCP servers synced successfully")
	i18n.Success(i18n.SuccessMCPServerSynced).With("status", "success").Send(c)
}

// HandleGetConfigVersions handles the request to get configuration versions
func (h *MCP) HandleGetConfigVersions(c *gin.Context) {
	configNames := c.QueryArray("names")
	tenant := c.Query("tenant")
	var versions []*config.MCPConfigVersion
	var configs []*config.MCPConfig

	// Get user from claims
	claims, exists := c.Get("claims")
	if !exists {
		h.logger.Warn("missing JWT claims in context")
		i18n.RespondWithError(c, i18n.ErrUnauthorized)
		return
	}
	jwtClaims := claims.(*jwt.Claims)

	// Get user information
	user, err := h.db.GetUserByUsername(c.Request.Context(), jwtClaims.Username)
	if err != nil {
		h.logger.Error("failed to get user info",
			zap.String("username", jwtClaims.Username),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user info: "+err.Error()))
		return
	}

	// If no names provided, get all configs first
	configs, err = h.store.List(c.Request.Context(), true)
	if err != nil {
		h.logger.Error("failed to list configs", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer)
		return
	}

	// Filter by tenant if specified
	if tenant != "" {
		filteredConfigs := make([]*config.MCPConfig, 0)
		for _, cfg := range configs {
			if cfg.Tenant == tenant {
				filteredConfigs = append(filteredConfigs, cfg)
			}
		}
		configs = filteredConfigs
	}

	if user.Role != database.RoleAdmin {
		// Get user's tenants
		tenants, err := h.db.GetUserTenants(c.Request.Context(), user.ID)
		if err != nil {
			h.logger.Error("failed to get user tenants",
				zap.String("username", user.Username),
				zap.Error(err))
			i18n.RespondWithError(c, i18n.ErrInternalServer)
			return
		}

		// Create a map of tenant names for faster lookup
		userTenants := make(map[string]bool)
		for _, t := range tenants {
			userTenants[t.Name] = true
		}

		filteredConfigs := make([]*config.MCPConfig, 0)
		for _, cfg := range configs {
			if userTenants[cfg.Tenant] {
				filteredConfigs = append(filteredConfigs, cfg)
			}
		}
		configs = filteredConfigs
	}

	if len(configNames) > 0 {
		// Filter configs by names
		filteredConfigs := make([]*config.MCPConfig, 0)
		for _, cfg := range configs {
			for _, name := range configNames {
				if cfg.Name == name {
					filteredConfigs = append(filteredConfigs, cfg)
					break
				}
			}
		}
		configs = filteredConfigs
	}

	if len(configs) == 0 {
		h.logger.Warn("no configs found")
		i18n.Success(i18n.SuccessMCPConfigVersions).With("data", versions).Send(c)
		return
	}

	// Get versions for each config
	for _, cfg := range configs {
		configVersions, err := h.store.ListVersions(c.Request.Context(), cfg.Tenant, cfg.Name)
		if err != nil {
			h.logger.Error("failed to list versions", zap.String("config", cfg.Name), zap.Error(err))
			continue
		}
		versions = append(versions, configVersions...)
	}

	// Sort versions by created_at in descending order
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].CreatedAt.After(versions[j].CreatedAt)
	})

	i18n.Success(i18n.SuccessMCPConfigVersions).With("data", versions).Send(c)
}

// HandleSetActiveVersion handles setting a version as active
func (h *MCP) HandleSetActiveVersion(c *gin.Context) {
	tenant := c.Param("tenant")
	if tenant == "" {
		h.logger.Warn("MCP server tenant required but missing")
		i18n.RespondWithError(c, i18n.ErrorTenantRequired)
		return
	}
	name := c.Param("name")
	if name == "" {
		h.logger.Warn("config name required but missing")
		i18n.RespondWithError(c, i18n.ErrorMCPServerNameRequired)
		return
	}
	versionStr := c.Param("version")
	if versionStr == "" {
		h.logger.Warn("version required but missing")
		i18n.RespondWithError(c, i18n.ErrorVersionRequired)
		return
	}

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		h.logger.Warn("invalid version number",
			zap.String("version", versionStr),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrBadRequest.WithParam("Reason", "Invalid version number"))
		return
	}

	h.logger.Info("handling set active version request",
		zap.String("config_name", name),
		zap.Int("version", version))

	// Get the config
	existingCfg, err := h.store.Get(c.Request.Context(), tenant, name)
	if err != nil {
		h.logger.Error("failed to get config",
			zap.String("config_name", name),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get config: "+err.Error()))
		return
	}

	// Check tenant permission
	_, err = h.checkTenantPermission(c, existingCfg.Tenant, existingCfg)
	if err != nil {
		h.logger.Warn("tenant permission check failed",
			zap.String("tenant", existingCfg.Tenant),
			zap.Error(err))
		i18n.RespondWithError(c, err)
		return
	}

	// Set version as active in store
	if err := h.store.SetActiveVersion(c.Request.Context(), existingCfg.Tenant, name, version); err != nil {
		h.logger.Error("failed to set active version",
			zap.String("config_name", name),
			zap.Int("version", version),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to set active version: "+err.Error()))
		return
	}

	// Send reload signal to gateway using notifier
	if err := h.notifier.NotifyUpdate(c.Request.Context(), existingCfg); err != nil {
		h.logger.Error("failed to notify gateway", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to notify gateway: "+err.Error()))
		return
	}

	h.logger.Info("active version set successfully",
		zap.String("config_name", name),
		zap.Int("version", version))
	i18n.Success(i18n.SuccessMCPServerUpdated).With("status", "success").Send(c)
}

// HandleGetConfigNames handles the request to get all configuration names
func (h *MCP) HandleGetConfigNames(c *gin.Context) {
	includeDeleted := c.Query("includeDeleted") == "true"
	tenant := c.Query("tenant")

	configs, err := h.store.List(c.Request.Context(), includeDeleted)
	if err != nil {
		h.logger.Error("failed to list configs", zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to list configs: "+err.Error()))
		return
	}

	// Get user from claims
	claims, exists := c.Get("claims")
	if !exists {
		h.logger.Warn("missing JWT claims in context")
		i18n.RespondWithError(c, i18n.ErrUnauthorized)
		return
	}
	jwtClaims := claims.(*jwt.Claims)

	// Get user information
	user, err := h.db.GetUserByUsername(c.Request.Context(), jwtClaims.Username)
	if err != nil {
		h.logger.Error("failed to get user info",
			zap.String("username", jwtClaims.Username),
			zap.Error(err))
		i18n.RespondWithError(c, i18n.ErrInternalServer.WithParam("Reason", "Failed to get user info: "+err.Error()))
		return
	}

	// Filter configs by tenant if user is not admin
	if user.Role != database.RoleAdmin {
		// Get user's tenants
		tenants, err := h.db.GetUserTenants(c.Request.Context(), user.ID)
		if err != nil {
			h.logger.Error("failed to get user tenants",
				zap.String("username", user.Username),
				zap.Error(err))
			i18n.RespondWithError(c, i18n.ErrInternalServer)
			return
		}

		// Create a map of tenant names for faster lookup
		userTenants := make(map[string]bool)
		for _, t := range tenants {
			userTenants[t.Name] = true
		}

		filteredConfigs := make([]*config.MCPConfig, 0)
		for _, cfg := range configs {
			if userTenants[cfg.Tenant] {
				filteredConfigs = append(filteredConfigs, cfg)
			}
		}
		configs = filteredConfigs
	}

	// Filter by tenant if specified
	if tenant != "" {
		filteredConfigs := make([]*config.MCPConfig, 0)
		for _, cfg := range configs {
			if cfg.Tenant == tenant {
				filteredConfigs = append(filteredConfigs, cfg)
			}
		}
		configs = filteredConfigs
	}

	// Extract unique config names
	names := make([]string, 0, len(configs))
	for _, cfg := range configs {
		names = append(names, cfg.Name)
	}

	c.JSON(http.StatusOK, gin.H{
		"data": names,
	})
}
