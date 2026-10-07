// Package http exposes device platform health and internal management endpoints.
package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/packages/go/httpapi"
	"github.com/TissyBoxC/sprout-platform/packages/go/observability"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	gatewayservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/service"
	auditdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	audithandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/handler"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	childservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/service"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	operationsservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/service"
	policeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/service"
	releasestoreservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
	serviceversionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
)

// RouterOptions contains dependencies for the device platform HTTP transport.
type RouterOptions struct {
	Logger                *slog.Logger
	InternalAPIConfig     config.InternalAPIConfig
	AuthService           *authservice.Service
	ChildService          *childservice.Service
	ParentPolicyService   *policeservice.Service
	AIService             *gatewayservice.Service
	OperationsService     *operationsservice.Service
	BindingService        *bindingservice.Service
	RuntimeService        *runtimeservice.Service
	DiagnosticService     auditService
	ServiceVersionService *serviceversionservice.Service
	ReleaseStoreService   *releasestoreservice.Service
	VoiceTokenIssuer      bindingservice.VoiceTokenIssuer
	VoiceWebSocketURL     string
}

// auditService is the diagnostic read surface exposed to administrators.
// Keeping the transport dependency narrow allows handlers to be tested with a
// small fake and avoids coupling HTTP to the database repository.
type auditService interface {
	Get(ctx context.Context, deviceID string, limit int) (*auditdomain.Snapshot, error)
	Validate(
		deviceID string,
		diagnostics *auditdomain.Diagnostics,
	) (*auditdomain.Diagnostics, error)
	Record(
		ctx context.Context,
		deviceID string,
		reportedAt time.Time,
		diagnostics *auditdomain.Diagnostics,
	) error
}

// NewRouter returns the HTTP router for the device platform.
func NewRouter(options RouterOptions) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler)

	if options.AuthService != nil {
		authHandler := authHandler{service: options.AuthService}
		mux.HandleFunc("POST /api/v1/auth/register", authHandler.register)
		mux.HandleFunc("POST /api/v1/auth/login", authHandler.login)
		mux.HandleFunc(
			"POST /api/v1/auth/phone-verification",
			authHandler.sendPhoneVerification,
		)
		mux.HandleFunc("POST /api/v1/admin/auth/login", authHandler.startAdminLogin)
		mux.HandleFunc("POST /api/v1/admin/auth/mfa", authHandler.completeAdminLogin)
		mux.HandleFunc(
			"PUT /api/v1/auth/email",
			authHandler.requireAuthentication(authHandler.bindEmail),
		)
		mux.HandleFunc(
			"PUT /api/v1/auth/profile",
			authHandler.requireAuthentication(authHandler.updateProfile),
		)
		mux.HandleFunc(
			"GET /api/v1/auth/overview",
			authHandler.requireAuthentication(authHandler.parentOverview),
		)
		mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.refresh)
		mux.HandleFunc("POST /api/v1/auth/logout", authHandler.logout)
		mux.HandleFunc(
			"GET /api/v1/auth/me",
			authHandler.requireAuthentication(authHandler.me),
		)
		if options.ChildService != nil && options.ParentPolicyService != nil {
			childHandler := childHandler{
				service:       options.ChildService,
				policyService: options.ParentPolicyService,
			}
			mux.HandleFunc(
				"GET /api/v1/children",
				authHandler.requireAuthentication(childHandler.list),
			)
			mux.HandleFunc(
				"POST /api/v1/children",
				authHandler.requireAuthentication(childHandler.create),
			)
			mux.HandleFunc(
				"PUT /api/v1/children/{child_id}",
				authHandler.requireAuthentication(childHandler.update),
			)
			mux.HandleFunc(
				"DELETE /api/v1/children/{child_id}",
				authHandler.requireAuthentication(childHandler.delete),
			)
			mux.HandleFunc(
				"GET /api/v1/children/{child_id}/policy",
				authHandler.requireAuthentication(childHandler.getPolicy),
			)
			mux.HandleFunc(
				"PUT /api/v1/children/{child_id}/policy",
				authHandler.requireAuthentication(childHandler.updatePolicy),
			)
		}
		if options.BindingService != nil {
			bindingHandler := deviceBindingHandler{
				service:           options.BindingService,
				voiceTokenIssuer:  options.VoiceTokenIssuer,
				voiceWebSocketURL: strings.TrimSpace(options.VoiceWebSocketURL),
			}
			mux.HandleFunc(
				"POST /api/v1/devices/register",
				bindingHandler.registerDevice,
			)
			mux.HandleFunc(
				"POST /api/v1/devices/auth/challenge",
				bindingHandler.startDeviceAuthentication,
			)
			mux.HandleFunc(
				"POST /api/v1/devices/auth/complete",
				bindingHandler.completeDeviceAuthentication,
			)
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/provisioning-token",
				bindingHandler.createDeviceProvisioningToken,
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/binding-status",
				bindingHandler.bindingStatus,
			)
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/voice-token",
				bindingHandler.createVoiceToken,
			)
			mux.HandleFunc(
				"POST /api/v1/devices/bind",
				authHandler.requireAuthentication(bindingHandler.bind),
			)
			mux.HandleFunc(
				"GET /api/v1/devices",
				authHandler.requireAuthentication(bindingHandler.list),
			)
			mux.HandleFunc(
				"DELETE /api/v1/devices/{device_id}",
				authHandler.requireAuthentication(bindingHandler.remove),
			)
		}
		if options.RuntimeService != nil {
			runtimeHandler := deviceRuntimeHandler{
				service:           options.RuntimeService,
				policyService:     options.ParentPolicyService,
				diagnosticService: options.DiagnosticService,
			}
			mux.HandleFunc(
				"GET /api/v1/devices/status",
				authHandler.requireAuthentication(runtimeHandler.listForParent),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/status",
				authHandler.requireAuthentication(runtimeHandler.getForParent),
			)
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/runtime/heartbeat",
				runtimeHandler.recordHeartbeat,
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/runtime/commands",
				runtimeHandler.listDeviceCommands,
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/runtime/parent-policy",
				runtimeHandler.getParentPolicy,
			)
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/runtime/commands/{command_id}/ack",
				runtimeHandler.acknowledgeCommand,
			)
			mux.HandleFunc(
				"GET /api/v1/admin/devices",
				authHandler.requireAdmin(runtimeHandler.listForAdmin),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/devices/{device_id}/commands",
				authHandler.requireAdmin(runtimeHandler.createCommand),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/devices/{device_id}/commands",
				authHandler.requireAdmin(runtimeHandler.listCommands),
			)
			if options.DiagnosticService != nil {
				diagnosticHandler := audithandler.New(options.DiagnosticService)
				mux.HandleFunc(
					"GET /api/v1/admin/devices/{device_id}/diagnostics",
					authHandler.requireAdmin(diagnosticHandler.GetDiagnostics),
				)
			}
		}
		if options.AIService != nil {
			adminHandler := adminHandler{
				service:               options.AIService,
				parentService:         options.AuthService,
				operationsService:     options.OperationsService,
				serviceVersionService: options.ServiceVersionService,
				releaseStoreService:   options.ReleaseStoreService,
				deviceStatusService:   options.RuntimeService,
				deviceBindingService:  options.BindingService,
				childService:          options.ChildService,
				policyService:         options.ParentPolicyService,
			}
			mux.HandleFunc(
				"PUT /api/v1/auth/ai-models",
				authHandler.requireAuthentication(adminHandler.updateParentAIModels),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families",
				authHandler.requireAdmin(adminHandler.listFamilies),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/families",
				authHandler.requireAdmin(adminHandler.createParent),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/families/{parent_account_id}/ai-account",
				authHandler.requireAdmin(adminHandler.retryParentAIAccount),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/families/{parent_account_id}/profile",
				authHandler.requireAdmin(adminHandler.updateParentProfile),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/families/{parent_account_id}/password",
				authHandler.requireAdmin(adminHandler.resetParentPassword),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families/{parent_account_id}/devices",
				authHandler.requireAdmin(adminHandler.listParentDevices),
			)
			mux.HandleFunc(
				"DELETE /api/v1/admin/families/{parent_account_id}/devices/{device_id}",
				authHandler.requireAdmin(adminHandler.unbindParentDevice),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families/{parent_account_id}/children",
				authHandler.requireAdmin(adminHandler.listFamilyChildren),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/ai-accounts",
				authHandler.requireAdmin(adminHandler.listAIAccounts),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/ai-account-defaults",
				authHandler.requireAdmin(adminHandler.getAIAccountDefaults),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/ai-models",
				authHandler.requireAdmin(adminHandler.listAIModels),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/ai-accounts/{provider_account_id}",
				authHandler.requireAdmin(adminHandler.updateAIAccount),
			)
			if options.OperationsService != nil {
				mux.HandleFunc(
					"GET /api/v1/admin/overview",
					authHandler.requireAdmin(adminHandler.getOverview),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/settings",
					authHandler.requireAdmin(adminHandler.getSettings),
				)
				mux.HandleFunc(
					"PUT /api/v1/admin/settings",
					authHandler.requireAdmin(adminHandler.updateSettings),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/releases",
					authHandler.requireAdmin(adminHandler.listReleases),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/releases",
					authHandler.requireAdmin(adminHandler.createRelease),
				)
				mux.HandleFunc(
					"PUT /api/v1/admin/releases/{version}/publish",
					authHandler.requireAdmin(adminHandler.publishRelease),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/releases/{version}",
					authHandler.requireAdmin(adminHandler.deleteRelease),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/release-artifacts/{version}",
					authHandler.requireAdmin(adminHandler.getReleaseArtifact),
				)
				mux.HandleFunc(
					"GET /api/v1/app/update",
					adminHandler.appUpdate,
				)
			}
			if options.ReleaseStoreService != nil {
				mux.HandleFunc(
					"GET /api/v1/admin/storage/files",
					authHandler.requireAdmin(adminHandler.listReleaseFiles),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/storage/files/upload",
					authHandler.requireAdmin(adminHandler.uploadReleaseFile),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/storage/files/{path...}",
					authHandler.requireAdmin(adminHandler.deleteReleaseFile),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/storage/index/status",
					authHandler.requireAdmin(adminHandler.getReleaseIndexStatus),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/storage/index/refresh",
					authHandler.requireAdmin(adminHandler.refreshReleaseIndex),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/release-files",
					authHandler.requireAdmin(adminHandler.listReleaseFiles),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/release-files",
					authHandler.requireAdmin(adminHandler.uploadReleaseFile),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/release-files",
					authHandler.requireAdmin(adminHandler.deleteReleaseFile),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/release-index/refresh",
					authHandler.requireAdmin(adminHandler.refreshReleaseIndex),
				)
			}
			if options.ServiceVersionService != nil {
				mux.HandleFunc(
					"GET /api/v1/admin/service-versions",
					authHandler.requireAdmin(adminHandler.listServiceVersions),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/service-versions/check",
					authHandler.requireAdmin(adminHandler.checkServiceVersions),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/service-versions/upgrade",
					authHandler.requireAdmin(adminHandler.upgradeAllServices),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/service-versions/{service}/upgrade",
					authHandler.requireAdmin(adminHandler.upgradeService),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/service-versions/{service}/releases",
					authHandler.requireAdmin(adminHandler.listServiceReleases),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/service-version-operations",
					authHandler.requireAdmin(adminHandler.listServiceVersionOperations),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/service-version-operations/{operation_id}",
					authHandler.requireAdmin(adminHandler.getServiceVersionOperation),
				)
			}
		}
	}

	if options.InternalAPIConfig.Enabled {
		mux.Handle(
			"GET /internal/v1/runtime",
			requireServiceToken(
				options.InternalAPIConfig.AuthToken,
				http.HandlerFunc(runtimeHandler),
			),
		)
		if options.BindingService != nil {
			bindingHandler := deviceBindingHandler{service: options.BindingService}
			mux.Handle(
				"POST /internal/v1/device-registration-tokens",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(bindingHandler.createRegistrationToken),
				),
			)
		}
		if options.BindingService != nil && options.AIService != nil {
			internalHandler := internalHandler{
				bindingService: options.BindingService,
				aiService:      options.AIService,
			}
			mux.Handle(
				"GET /internal/v1/devices/{device_id}/ai-credential",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(internalHandler.aiCredential),
				),
			)
		}
		if options.ParentPolicyService != nil {
			internalHandler := internalHandler{
				policyService: options.ParentPolicyService,
			}
			mux.Handle(
				"GET /internal/v1/parent-policies",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(internalHandler.effectivePolicies),
				),
			)
		}
		if options.ReleaseStoreService != nil &&
			strings.TrimSpace(options.InternalAPIConfig.ReleaseUploadToken) != "" {
			internalHandler := internalHandler{
				releaseStoreService: options.ReleaseStoreService,
			}
			// Release automation only needs the constrained download-store
			// surface. Keeping these routes separate from admin routes avoids
			// granting a CI credential access to family or provider data.
			mux.Handle(
				"POST /internal/v1/release-files",
				requireServiceToken(
					options.InternalAPIConfig.ReleaseUploadToken,
					http.HandlerFunc(internalHandler.uploadReleaseFile),
				),
			)
			mux.Handle(
				"POST /api/v1/release-publication/files",
				requireServiceToken(
					options.InternalAPIConfig.ReleaseUploadToken,
					http.HandlerFunc(internalHandler.uploadReleaseFile),
				),
			)
			mux.Handle(
				"POST /internal/v1/release-index/refresh",
				requireServiceToken(
					options.InternalAPIConfig.ReleaseUploadToken,
					http.HandlerFunc(internalHandler.refreshReleaseIndex),
				),
			)
			mux.Handle(
				"POST /api/v1/release-publication/index-refresh",
				requireServiceToken(
					options.InternalAPIConfig.ReleaseUploadToken,
					http.HandlerFunc(internalHandler.refreshReleaseIndex),
				),
			)
		}
	}

	return observability.WithRequestLabels(observability.WithRequestMetadata(
		observability.WithAccessLog(
			mux,
			observability.AccessLogOptions{
				Logger:      logger,
				ServiceName: "device-platform",
				Audit:       observability.NewSlogAuditSink(logger),
			},
		),
	))
}

func healthHandler(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "device-platform",
		"time":    time.Now().UTC(),
	})
}

func readyHandler(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{
		"status": "ready",
	})
}

func runtimeHandler(response http.ResponseWriter, request *http.Request) {
	httpapi.WriteSuccess(response, request, http.StatusOK, map[string]any{
		"service":          "device-platform",
		"status":           "running",
		"protocol_version": httpapi.SchemaVersion,
	})
}

func requireServiceToken(expectedToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		providedToken := request.Header.Get("Authorization")
		const bearerPrefix = "Bearer "
		if len(providedToken) <= len(bearerPrefix) ||
			providedToken[:len(bearerPrefix)] != bearerPrefix {
			httpapi.WriteError(
				response,
				request,
				http.StatusUnauthorized,
				"unauthenticated",
				"服务认证失败",
				false,
			)
			return
		}

		providedToken = providedToken[len(bearerPrefix):]
		if subtle.ConstantTimeCompare([]byte(providedToken), []byte(expectedToken)) != 1 {
			httpapi.WriteError(
				response,
				request,
				http.StatusUnauthorized,
				"unauthenticated",
				"服务认证失败",
				false,
			)
			return
		}

		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(payload)
}
