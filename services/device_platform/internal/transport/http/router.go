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
	companionshipservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/service"
	connectivityservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/service"
	contentservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/service"
	contentofflinepolicyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/service"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	devicemeshservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/service"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	displayservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/service"
	englishservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/service"
	featurecenterservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	formfactorservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/service"
	inputservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/service"
	localcommandservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/service"
	motionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/motion/service"
	notificationhandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/handler"
	notificationservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/service"
	operationsservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/service"
	otaservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/service"
	policeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/service"
	playbackservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/service"
	powerservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/service"
	privacyhandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/handler"
	privacyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/service"
	recommendationservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/service"
	releasestoreservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
	serviceversionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
	usagereportservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/service"
	videoservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/service"
	visionhandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/handler"
	visionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/service"
	wearableservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/service"
)

// RouterOptions contains dependencies for the device platform HTTP transport.
type RouterOptions struct {
	Logger                      *slog.Logger
	InternalAPIConfig           config.InternalAPIConfig
	AuthService                 *authservice.Service
	ChildService                *childservice.Service
	CompanionshipService        *companionshipservice.Service
	ParentPolicyService         *policeservice.Service
	AIService                   *gatewayservice.Service
	OperationsService           *operationsservice.Service
	BindingService              *bindingservice.Service
	RuntimeService              *runtimeservice.Service
	DiagnosticService           auditService
	ServiceVersionService       *serviceversionservice.Service
	FeatureCenterService        *featurecenterservice.Service
	ReleaseStoreService         *releasestoreservice.Service
	OTAService                  *otaservice.Service
	ContentService              *contentservice.Service
	ContentOfflinePolicyService *contentofflinepolicyservice.Service
	EnglishService              *englishservice.Service
	PlaybackService             *playbackservice.Service
	LocalCommandService         *localcommandservice.Service
	RecommendationService       *recommendationservice.Service
	UsageReportService          *usagereportservice.Service
	VoiceTokenIssuer            bindingservice.VoiceTokenIssuer
	VoiceWebSocketURL           string
	PrivacyService              *privacyservice.Service
	NotificationService         *notificationservice.Service
	VisionService               *visionservice.Service
	DisplayService              *displayservice.Service
	InputService                *inputservice.Service
	ConnectivityService         *connectivityservice.Service
	PowerService                *powerservice.Service
	MotionService               *motionservice.Service
	FormFactorService           *formfactorservice.Service
	VideoCompanionService       *videoservice.Service
	WearableService             *wearableservice.Service
	DeviceMeshService           *devicemeshservice.Service
	AuditRecorder               auditRecorder
}

// auditRecorder is the narrow operation-audit write surface used by existing
// administrator handlers. The privacy service implements it; transport does
// not depend on the privacy repository or domain internals.
type auditRecorder interface {
	RecordAudit(
		ctx context.Context,
		actorAccountID string,
		targetAccountID string,
		action string,
		detail map[string]any,
	) error
}

// auditService is the diagnostic read surface exposed to administrators.
// Keeping the transport dependency narrow allows handlers to be tested with a
// small fake and avoids coupling HTTP to the database repository.
type auditService interface {
	Get(ctx context.Context, deviceID string, limit int) (*auditdomain.Snapshot, error)
	GetSummary(
		ctx context.Context,
		deviceID string,
		windowDays int,
	) (*auditdomain.Summary, error)
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
	GetProvisioning(
		ctx context.Context,
		deviceID string,
		limit int,
	) (*auditdomain.ProvisioningSnapshot, error)
	ValidateProvisioning(
		deviceID string,
		provisioning *auditdomain.Provisioning,
	) (*auditdomain.Provisioning, error)
	RecordProvisioning(
		ctx context.Context,
		deviceID string,
		reportedAt time.Time,
		provisioning *auditdomain.Provisioning,
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
		if options.CompanionshipService != nil {
			companionshipHandler := companionshipHandler{
				service: options.CompanionshipService,
			}
			mux.HandleFunc(
				"GET /api/v1/companionship/growth-records",
				authHandler.requireAuthentication(companionshipHandler.listGrowthRecords),
			)
			mux.HandleFunc(
				"POST /api/v1/companionship/growth-records",
				authHandler.requireAuthentication(companionshipHandler.createGrowthRecord),
			)
			mux.HandleFunc(
				"DELETE /api/v1/companionship/growth-records/{record_id}",
				authHandler.requireAuthentication(companionshipHandler.deleteGrowthRecord),
			)
			mux.HandleFunc(
				"GET /api/v1/companionship/habit-reminders",
				authHandler.requireAuthentication(companionshipHandler.listHabitReminders),
			)
			mux.HandleFunc(
				"POST /api/v1/companionship/habit-reminders",
				authHandler.requireAuthentication(companionshipHandler.createHabitReminder),
			)
			mux.HandleFunc(
				"PUT /api/v1/companionship/habit-reminders/{reminder_id}",
				authHandler.requireAuthentication(companionshipHandler.updateHabitReminder),
			)
			mux.HandleFunc(
				"DELETE /api/v1/companionship/habit-reminders/{reminder_id}",
				authHandler.requireAuthentication(companionshipHandler.deleteHabitReminder),
			)
		}
		if options.EnglishService != nil {
			englishHandler := englishHandler{
				service: options.EnglishService,
			}
			mux.HandleFunc(
				"GET /api/v1/english/lessons",
				authHandler.requireAuthentication(englishHandler.listLessons),
			)
			mux.HandleFunc(
				"GET /api/v1/english/lessons/{lesson_id}",
				authHandler.requireAuthentication(englishHandler.getLesson),
			)
			mux.HandleFunc(
				"GET /api/v1/english/progress",
				authHandler.requireAuthentication(englishHandler.listProgress),
			)
			mux.HandleFunc(
				"GET /api/v1/english/attempts",
				authHandler.requireAuthentication(englishHandler.listAttempts),
			)
			// Firmware devices never receive a guardian credential, so the
			// practice-attempt write is also exposed behind a device session
			// token verified with the path device id.
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceEnglishHandler := deviceEnglishHandler{
					englishHandler: &englishHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/english/attempts",
					deviceEnglishHandler.recordAttempt,
				)
			}
		}
		if options.PlaybackService != nil {
			playbackHandler := playbackHandler{
				service: options.PlaybackService,
			}
			mux.HandleFunc(
				"POST /api/v1/playback/requests",
				authHandler.requireAuthentication(playbackHandler.create),
			)
			mux.HandleFunc(
				"GET /api/v1/playback/requests",
				authHandler.requireAuthentication(playbackHandler.list),
			)
			mux.HandleFunc(
				"GET /api/v1/playback/requests/{request_id}",
				authHandler.requireAuthentication(playbackHandler.get),
			)
			mux.HandleFunc(
				"POST /api/v1/playback/requests/{request_id}/cancel",
				authHandler.requireAuthentication(playbackHandler.cancel),
			)
			// Firmware devices never receive a guardian credential, so the
			// device-facing reads and outcome writes are exposed behind a
			// device session token verified with the path device id.
			if options.BindingService != nil && options.RuntimeService != nil {
				devicePlaybackHandler := devicePlaybackHandler{
					playbackHandler: &playbackHandler,
					bindingService:  options.BindingService,
					runtimeService:  options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/playback/requests",
					devicePlaybackHandler.listPending,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/playback/requests/{request_id}/acknowledge",
					devicePlaybackHandler.acknowledge,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/playback/requests/{request_id}/complete",
					devicePlaybackHandler.complete,
				)
			}
		}
		if options.RecommendationService != nil {
			recommendationHandler := recommendationHandler{
				service: options.RecommendationService,
			}
			mux.HandleFunc(
				"GET /api/v1/content/recommendations",
				authHandler.requireAuthentication(recommendationHandler.list),
			)
		}
		if options.LocalCommandService != nil {
			localCommandHandler := localCommandHandler{
				service: options.LocalCommandService,
			}
			mux.HandleFunc(
				"GET /api/v1/local-commands",
				authHandler.requireAuthentication(localCommandHandler.list),
			)
			mux.HandleFunc(
				"PUT /api/v1/local-commands",
				authHandler.requireAuthentication(localCommandHandler.replace),
			)
			mux.HandleFunc(
				"DELETE /api/v1/local-commands/{command_id}",
				authHandler.requireAuthentication(localCommandHandler.delete),
			)
			// Firmware devices read the family's command set with a device
			// session token so common actions keep working with no guardian
			// present.
			if options.BindingService != nil {
				deviceLocalCommandHandler := deviceLocalCommandHandler{
					handler:        &localCommandHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/local-commands",
					deviceLocalCommandHandler.listForDevice,
				)
			}
		}
		if options.ContentOfflinePolicyService != nil {
			offlinePolicyHandler := contentOfflinePolicyHandler{
				service: options.ContentOfflinePolicyService,
			}
			mux.HandleFunc(
				"GET /api/v1/content/offline-policy",
				authHandler.requireAuthentication(offlinePolicyHandler.current),
			)
			mux.HandleFunc(
				"PUT /api/v1/content/offline-policy",
				authHandler.requireAuthentication(offlinePolicyHandler.publish),
			)
			mux.HandleFunc(
				"GET /api/v1/content/offline-policy/acknowledgements",
				authHandler.requireAuthentication(offlinePolicyHandler.acknowledgements),
			)
			// Firmware devices read the fleet policy with a device session
			// token so the cache budget is enforced without a guardian present.
			if options.BindingService != nil {
				deviceOfflinePolicyHandler := deviceContentOfflinePolicyHandler{
					handler:        &offlinePolicyHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/content/offline-policy",
					deviceOfflinePolicyHandler.currentForDevice,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/content/offline-policy/acknowledgement",
					deviceOfflinePolicyHandler.acknowledgeForDevice,
				)
			}
		}
		if options.DisplayService != nil {
			displayHandler := displayHandler{service: options.DisplayService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceDisplayHandler := deviceDisplayHandler{
					handler:        &displayHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/display/status",
					deviceDisplayHandler.reportStatus,
				)
			}
		}
		if options.InputService != nil {
			inputHandler := inputHandler{service: options.InputService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceInputHandler := deviceInputHandler{
					handler:        &inputHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/touch/status",
					deviceInputHandler.reportStatus,
				)
			}
		}
		if options.ConnectivityService != nil {
			connectivityHandler := connectivityHandler{service: options.ConnectivityService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceConnectivityHandler := deviceConnectivityHandler{
					handler:        &connectivityHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/connectivity/status",
					deviceConnectivityHandler.reportStatus,
				)
			}
		}
		if options.PowerService != nil {
			powerHandler := powerHandler{service: options.PowerService}
			if options.BindingService != nil && options.RuntimeService != nil {
				devicePowerHandler := devicePowerHandler{
					handler:        &powerHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/power/status",
					devicePowerHandler.reportStatus,
				)
			}
		}
		if options.MotionService != nil {
			motionHandler := motionHandler{service: options.MotionService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceMotionHandler := deviceMotionHandler{
					handler:        &motionHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/motion/status",
					deviceMotionHandler.reportStatus,
				)
			}
		}
		if options.FormFactorService != nil {
			formFactorHandler := formFactorHandler{service: options.FormFactorService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceFormFactorHandler := deviceFormFactorHandler{
					handler:        &formFactorHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/form-factor/report",
					deviceFormFactorHandler.report,
				)
			}
		}
		if options.VideoCompanionService != nil {
			videoHandler := videoCompanionHandler{service: options.VideoCompanionService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceVideoHandler := deviceVideoHandler{
					handler:        &videoHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/video-companion/status",
					deviceVideoHandler.reportStatus,
				)
			}
		}
		if options.WearableService != nil {
			wearableHandler := wearableHandler{service: options.WearableService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceWearableHandler := deviceWearableHandler{
					handler:        &wearableHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/wearable/location",
					deviceWearableHandler.reportLocation,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/wearable/sos",
					deviceWearableHandler.reportSOS,
				)
			}
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
				bindingRevoker:    options.BindingService,
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
			if options.VisionService != nil {
				deviceVisionHandler := deviceVisionHandler{
					service:        options.VisionService,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/tasks/pending",
					deviceVisionHandler.listPendingTasks,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks/{task_id}/acknowledge",
					deviceVisionHandler.acknowledgeTask,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks/{task_id}/status",
					deviceVisionHandler.reportStatus,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/results",
					deviceVisionHandler.reportResult,
				)
			}
			if options.UsageReportService != nil {
				usageHandler := usageReportHandler{
					service:        options.UsageReportService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/runtime/usage",
					usageHandler.recordDeviceUsage,
				)
			}
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
				mux.HandleFunc(
					"GET /api/v1/admin/devices/{device_id}/diagnostics/summary",
					authHandler.requireAdmin(diagnosticHandler.GetSummary),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/devices/{device_id}/provisioning",
					authHandler.requireAdmin(diagnosticHandler.GetProvisioning),
				)
			}
			mux.HandleFunc(
				"POST /api/v1/admin/devices/{device_id}/sessions/revoke",
				authHandler.requireAdmin(runtimeHandler.revokeSessions),
			)
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
				auditService:          options.AuditRecorder,
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
			if options.UsageReportService != nil {
				usageHandler := usageReportHandler{service: options.UsageReportService}
				mux.HandleFunc(
					"GET /api/v1/admin/families/{parent_account_id}/usage-reports",
					authHandler.requireAdmin(usageHandler.listAdminFamilyUsageReports),
				)
			}
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
			if options.FeatureCenterService != nil {
				featureCenterHandler := NewFeatureCenterHandler(options.FeatureCenterService)
				featureCenterHandler.RegisterAdminRoutes(
					mux,
					authHandler.requireAdmin,
				)
			}
			// parentScoped resolves the authenticated guardian id and is shared
			// by guardian-owned routes such as privacy and notifications.
			parentScoped := func(
				action func(
					http.ResponseWriter,
					*http.Request,
					string,
				),
			) http.HandlerFunc {
				return func(response http.ResponseWriter, request *http.Request) {
					accountID, ok := authenticatedAccountID(request)
					if !ok {
						writeError(
							response,
							request,
							http.StatusUnauthorized,
							"unauthenticated",
							"请重新登录",
						)
						return
					}
					action(response, request, accountID)
				}
			}
			// adminScoped resolves the authenticated administrator id for
			// attribution and audit.
			adminScoped := func(
				action func(
					http.ResponseWriter,
					*http.Request,
					string,
				),
			) http.HandlerFunc {
				return func(response http.ResponseWriter, request *http.Request) {
					accountID, _ := authenticatedAccountID(request)
					action(response, request, accountID)
				}
			}
			if options.PrivacyService != nil {
				privacyHandler := privacyhandler.New(options.PrivacyService)
				mux.HandleFunc(
					"GET /api/v1/privacy",
					authHandler.requireAuthentication(parentScoped(privacyHandler.Status)),
				)
				mux.HandleFunc(
					"GET /api/v1/privacy/export",
					authHandler.requireAuthentication(parentScoped(privacyHandler.Export)),
				)
				mux.HandleFunc(
					"POST /api/v1/privacy/deletion",
					authHandler.requireAuthentication(
						parentScoped(privacyHandler.RequestDeletion),
					),
				)
				mux.HandleFunc(
					"DELETE /api/v1/privacy/deletion",
					authHandler.requireAuthentication(
						parentScoped(privacyHandler.CancelDeletion),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/privacy/consent/withdraw",
					authHandler.requireAuthentication(
						parentScoped(privacyHandler.WithdrawConsent),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/privacy/consent/grant",
					authHandler.requireAuthentication(
						parentScoped(privacyHandler.GrantConsent),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/audit",
					authHandler.requireAdmin(privacyHandler.AdminAudit),
				)
			}
			if options.VisionService != nil {
				visionHandler := visionhandler.New(options.VisionService)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/capability",
					authHandler.requireAuthentication(visionHandler.GetCapability),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks",
					authHandler.requireAuthentication(
						parentScoped(visionHandler.CreateTask),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/tasks",
					authHandler.requireAuthentication(
						parentScoped(visionHandler.ListTasks),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks/{task_id}/cancel",
					authHandler.requireAuthentication(
						parentScoped(func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							visionHandler.CancelTask(response, request)
						}),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/results",
					authHandler.requireAuthentication(
						parentScoped(visionHandler.ListResults),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/results/{task_id}",
					authHandler.requireAuthentication(
						parentScoped(visionHandler.GetResult),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/vision/stats",
					authHandler.requireAdmin(visionHandler.GetAdminStats),
				)
			}
			if options.DisplayService != nil {
				displayHandler := displayHandler{service: options.DisplayService}
				mux.HandleFunc(
					"GET /api/v1/display/policy",
					authHandler.requireAuthentication(displayHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/display/policy",
					authHandler.requireAuthentication(displayHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/display/devices",
					authHandler.requireAuthentication(displayHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/display/capability",
					authHandler.requireAuthentication(displayHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/display/stats",
					authHandler.requireAdmin(displayHandler.adminStats),
				)
			}
			if options.InputService != nil {
				inputHandler := inputHandler{service: options.InputService}
				mux.HandleFunc(
					"GET /api/v1/touch/config",
					authHandler.requireAuthentication(inputHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/touch/config",
					authHandler.requireAuthentication(inputHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/touch/devices",
					authHandler.requireAuthentication(inputHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/touch/capability",
					authHandler.requireAuthentication(inputHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/touch/stats",
					authHandler.requireAdmin(inputHandler.adminStats),
				)
			}
			if options.ConnectivityService != nil {
				connectivityHandler := connectivityHandler{service: options.ConnectivityService}
				mux.HandleFunc(
					"GET /api/v1/connectivity/policy",
					authHandler.requireAuthentication(connectivityHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/connectivity/policy",
					authHandler.requireAuthentication(connectivityHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/connectivity/devices",
					authHandler.requireAuthentication(connectivityHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/connectivity/capability",
					authHandler.requireAuthentication(connectivityHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/connectivity/stats",
					authHandler.requireAdmin(connectivityHandler.adminStats),
				)
			}
			if options.PowerService != nil {
				powerHandler := powerHandler{service: options.PowerService}
				mux.HandleFunc(
					"GET /api/v1/power/policy",
					authHandler.requireAuthentication(powerHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/power/policy",
					authHandler.requireAuthentication(powerHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/power/devices",
					authHandler.requireAuthentication(powerHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/power/capability",
					authHandler.requireAuthentication(powerHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/power/stats",
					authHandler.requireAdmin(powerHandler.adminStats),
				)
			}
			if options.MotionService != nil {
				motionHandler := motionHandler{service: options.MotionService}
				mux.HandleFunc(
					"GET /api/v1/motion/policy",
					authHandler.requireAuthentication(motionHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/motion/policy",
					authHandler.requireAuthentication(motionHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/motion/devices",
					authHandler.requireAuthentication(motionHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/motion/capability",
					authHandler.requireAuthentication(motionHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/motion/stats",
					authHandler.requireAdmin(motionHandler.adminStats),
				)
			}
			if options.FormFactorService != nil {
				formFactorHandler := formFactorHandler{service: options.FormFactorService}
				mux.HandleFunc(
					"GET /api/v1/form-factor/devices",
					authHandler.requireAuthentication(formFactorHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/form-factor/capability",
					authHandler.requireAuthentication(formFactorHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/form-factor/stats",
					authHandler.requireAdmin(formFactorHandler.adminStats),
				)
			}
			if options.VideoCompanionService != nil {
				videoHandler := videoCompanionHandler{service: options.VideoCompanionService}
				mux.HandleFunc(
					"GET /api/v1/video-companion/contacts",
					authHandler.requireAuthentication(videoHandler.listContacts),
				)
				mux.HandleFunc(
					"POST /api/v1/video-companion/contacts",
					authHandler.requireAuthentication(videoHandler.createContact),
				)
				mux.HandleFunc(
					"PUT /api/v1/video-companion/contacts/{contact_id}",
					authHandler.requireAuthentication(videoHandler.updateContact),
				)
				mux.HandleFunc(
					"DELETE /api/v1/video-companion/contacts/{contact_id}",
					authHandler.requireAuthentication(videoHandler.deleteContact),
				)
				mux.HandleFunc(
					"GET /api/v1/video-companion/calls",
					authHandler.requireAuthentication(videoHandler.history),
				)
				mux.HandleFunc(
					"POST /api/v1/video-companion/calls",
					authHandler.requireAuthentication(videoHandler.startCall),
				)
				mux.HandleFunc(
					"PUT /api/v1/video-companion/calls/{call_id}",
					authHandler.requireAuthentication(videoHandler.updateCall),
				)
				mux.HandleFunc(
					"GET /api/v1/video-companion/devices",
					authHandler.requireAuthentication(videoHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/video-companion/capability",
					authHandler.requireAuthentication(videoHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/video-companion/stats",
					authHandler.requireAdmin(videoHandler.adminStats),
				)
			}
			if options.WearableService != nil {
				wearableHandler := wearableHandler{service: options.WearableService}
				mux.HandleFunc(
					"GET /api/v1/wearable/policy",
					authHandler.requireAuthentication(wearableHandler.policy),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/policy",
					authHandler.requireAuthentication(wearableHandler.updatePolicy),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/contacts",
					authHandler.requireAuthentication(wearableHandler.listContacts),
				)
				mux.HandleFunc(
					"POST /api/v1/wearable/contacts",
					authHandler.requireAuthentication(wearableHandler.createContact),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/contacts/{contact_id}",
					authHandler.requireAuthentication(wearableHandler.updateContact),
				)
				mux.HandleFunc(
					"DELETE /api/v1/wearable/contacts/{contact_id}",
					authHandler.requireAuthentication(wearableHandler.deleteContact),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/geofence",
					authHandler.requireAuthentication(wearableHandler.geofence),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/geofence",
					authHandler.requireAuthentication(wearableHandler.updateGeofence),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/devices",
					authHandler.requireAuthentication(wearableHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/wearable/capability",
					authHandler.requireAuthentication(wearableHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/sos",
					authHandler.requireAuthentication(wearableHandler.sosHistory),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/sos/active",
					authHandler.requireAuthentication(wearableHandler.activeSOS),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/sos/{event_id}/cancel",
					authHandler.requireAuthentication(wearableHandler.cancelSOS),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/wearable/stats",
					authHandler.requireAdmin(wearableHandler.adminStats),
				)
			}
			if options.DeviceMeshService != nil {
				meshHandler := deviceMeshHandler{service: options.DeviceMeshService}
				mux.HandleFunc(
					"GET /api/v1/device-mesh/groups",
					authHandler.requireAuthentication(meshHandler.listGroups),
				)
				mux.HandleFunc(
					"POST /api/v1/device-mesh/groups",
					authHandler.requireAuthentication(meshHandler.createGroup),
				)
				mux.HandleFunc(
					"DELETE /api/v1/device-mesh/groups/{group_id}",
					authHandler.requireAuthentication(meshHandler.deleteGroup),
				)
				mux.HandleFunc(
					"GET /api/v1/device-mesh/groups/{group_id}/members",
					authHandler.requireAuthentication(meshHandler.listMembers),
				)
				mux.HandleFunc(
					"POST /api/v1/device-mesh/groups/{group_id}/members",
					authHandler.requireAuthentication(meshHandler.addMember),
				)
				mux.HandleFunc(
					"DELETE /api/v1/device-mesh/groups/{group_id}/members/{device_id}",
					authHandler.requireAuthentication(meshHandler.removeMember),
				)
				mux.HandleFunc(
					"GET /api/v1/device-mesh/messages",
					authHandler.requireAuthentication(meshHandler.listMessages),
				)
				mux.HandleFunc(
					"POST /api/v1/device-mesh/messages",
					authHandler.requireAuthentication(meshHandler.routeMessage),
				)
				mux.HandleFunc(
					"GET /api/v1/device-mesh/content",
					authHandler.requireAuthentication(meshHandler.listContentStates),
				)
				mux.HandleFunc(
					"PUT /api/v1/device-mesh/content",
					authHandler.requireAuthentication(meshHandler.reportSync),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/device-mesh/stats",
					authHandler.requireAdmin(meshHandler.adminStats),
				)
			}
			if options.NotificationService != nil {
				notificationHandler := notificationhandler.New(options.NotificationService)
				adminNotificationHandler := notificationhandler.NewAdmin(options.NotificationService)
				mux.HandleFunc(
					"GET /api/v1/notifications",
					authHandler.requireAuthentication(
						parentScoped(notificationHandler.Inbox),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/notifications/unread-count",
					authHandler.requireAuthentication(
						parentScoped(notificationHandler.UnreadCount),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/notifications/read-all",
					authHandler.requireAuthentication(
						parentScoped(notificationHandler.MarkAllRead),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/notifications/{notification_id}/read",
					authHandler.requireAuthentication(
						parentScoped(notificationHandler.MarkRead),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/notifications/family-messages",
					authHandler.requireAuthentication(
						parentScoped(notificationHandler.SendFamilyMessage),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/notifications/devices/{device_id}/messages",
					authHandler.requireAuthentication(
						parentScoped(notificationHandler.DeviceMessages),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/notifications",
					authHandler.requireAdmin(adminNotificationHandler.AdminList),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/notifications",
					authHandler.requireAdmin(adminScoped(adminNotificationHandler.AdminCreate)),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/notifications/stats",
					authHandler.requireAdmin(adminNotificationHandler.AdminStats),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/notifications/{notification_id}",
					authHandler.requireAdmin(adminNotificationHandler.AdminGet),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/notifications/{notification_id}",
					authHandler.requireAdmin(adminScoped(adminNotificationHandler.AdminDelete)),
				)
			}
		}
		if options.ContentService != nil {
			contentHandler := contentHandler{service: options.ContentService}
			mux.HandleFunc(
				"GET /api/v1/admin/content/packages",
				authHandler.requireAdmin(contentHandler.listPackages),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages",
				authHandler.requireAdmin(contentHandler.createPackage),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/packages/{package_id}",
				authHandler.requireAdmin(contentHandler.getPackage),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions",
				authHandler.requireAdmin(contentHandler.createPackageVersion),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/content/packages/{package_id}/versions/{package_version}",
				authHandler.requireAdmin(contentHandler.updatePackage),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/submit",
				authHandler.requireAdmin(contentHandler.submitPackage),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/approve",
				authHandler.requireAdmin(contentHandler.approvePackage),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/reject",
				authHandler.requireAdmin(contentHandler.rejectPackage),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/publish",
				authHandler.requireAdmin(contentHandler.publishPackage),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/withdraw",
				authHandler.requireAdmin(contentHandler.withdrawPackage),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/archive",
				authHandler.requireAdmin(contentHandler.archivePackage),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/theme-packs",
				authHandler.requireAdmin(contentHandler.listThemePacks),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/content/theme-packs/{theme_id}",
				authHandler.requireAdmin(contentHandler.upsertThemePack),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/theme-packs/{theme_id}",
				authHandler.requireAdmin(contentHandler.getThemePack),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/theme-packs/{theme_id}/publish",
				authHandler.requireAdmin(contentHandler.publishThemePack),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/theme-packs/{theme_id}/withdraw",
				authHandler.requireAdmin(contentHandler.withdrawThemePack),
			)
			mux.HandleFunc(
				"DELETE /api/v1/admin/content/theme-packs/{theme_id}",
				authHandler.requireAdmin(contentHandler.deleteThemePack),
			)
		}
		if options.ContentOfflinePolicyService != nil {
			adminOfflinePolicyHandler := contentOfflinePolicyHandler{
				service: options.ContentOfflinePolicyService,
			}
			mux.HandleFunc(
				"GET /api/v1/admin/content/offline-policy",
				authHandler.requireAdmin(adminOfflinePolicyHandler.adminCurrent),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/content/offline-policy",
				authHandler.requireAdmin(adminOfflinePolicyHandler.adminPublish),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/offline-policy/acknowledgements",
				authHandler.requireAdmin(adminOfflinePolicyHandler.acknowledgementsForAdmin),
			)
		}
	}

	if options.UsageReportService != nil && options.AuthService != nil {
		usageHandler := usageReportHandler{service: options.UsageReportService}
		authHandler := authHandler{service: options.AuthService}
		mux.HandleFunc(
			"GET /api/v1/usage-reports",
			authHandler.requireAuthentication(usageHandler.listGuardianUsageReports),
		)
	}

	if options.ContentService != nil {
		contentHandler := contentHandler{
			service:         options.ContentService,
			guardianDevices: guardianDeviceListerFromBinding(options.BindingService),
		}
		// The parent app reads the catalog with a guardian session.
		if options.AuthService != nil {
			authHandler := authHandler{service: options.AuthService}
			mux.HandleFunc(
				"GET /api/v1/content/catalog",
				authHandler.requireAuthentication(contentHandler.catalog),
			)
			mux.HandleFunc(
				"GET /api/v1/content/packages/{package_id}/download",
				authHandler.requireAuthentication(contentHandler.download),
			)
			mux.HandleFunc(
				"GET /api/v1/content/favorites",
				authHandler.requireAuthentication(contentHandler.listFavorites),
			)
			mux.HandleFunc(
				"PUT /api/v1/content/favorites/{package_id}",
				authHandler.requireAuthentication(contentHandler.addFavorite),
			)
			mux.HandleFunc(
				"DELETE /api/v1/content/favorites/{package_id}",
				authHandler.requireAuthentication(contentHandler.removeFavorite),
			)
			mux.HandleFunc(
				"GET /api/v1/content/play-history",
				authHandler.requireAuthentication(contentHandler.listPlayHistoryForGuardian),
			)
			mux.HandleFunc(
				"GET /api/v1/content/theme-packs",
				authHandler.requireAuthentication(contentHandler.listPublishedThemePacks),
			)
		}
		// Firmware devices never receive a guardian credential, so the same
		// manifest and download lookup are also exposed behind a device
		// session token verified with the path device id.
		if options.BindingService != nil {
			deviceHandler := deviceContentHandler{
				contentHandler: contentHandler,
				bindingService: options.BindingService,
				runtimeService: options.RuntimeService,
			}
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/content/catalog",
				deviceHandler.catalog,
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/content/packages/{package_id}/download",
				deviceHandler.download,
			)
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/content/playback",
				deviceHandler.recordPlayback,
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/content/playback",
				deviceHandler.listPlayHistory,
			)
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
		if options.ContentService != nil {
			contentHandler := contentHandler{service: options.ContentService}
			mux.Handle(
				"GET /internal/v1/content/catalog",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(contentHandler.catalog),
				),
			)
			mux.Handle(
				"GET /internal/v1/content/packages/{package_id}/download",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(contentHandler.download),
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

	if options.OTAService != nil {
		ota := newOTAHandler(
			options.OTAService,
			options.BindingService,
			guardianDeviceVerifierFromBinding(options.BindingService),
		)
		var requireAuthentication func(http.HandlerFunc) http.HandlerFunc
		var requireAdmin func(http.HandlerFunc) http.HandlerFunc
		if options.AuthService != nil {
			authHandler := authHandler{service: options.AuthService}
			requireAuthentication = authHandler.requireAuthentication
			requireAdmin = authHandler.requireAdmin
		}
		ota.registerRoutes(mux, requireAuthentication, requireAdmin)
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

func guardianDeviceVerifierFromBinding(
	bindingService *bindingservice.Service,
) GuardianDeviceVerifier {
	if bindingService == nil {
		return nil
	}
	return bindingGuardianVerifier{bindingService: bindingService}
}

// guardianDeviceListerFromBinding exposes the binding-derived resolver as a
// device-id lister for the guardian content surface. It returns nil when no
// binding service is configured so the route can fail closed.
func guardianDeviceListerFromBinding(
	bindingService *bindingservice.Service,
) guardianDeviceLister {
	if bindingService == nil {
		return nil
	}
	return bindingGuardianVerifier{bindingService: bindingService}
}

type bindingGuardianVerifier struct {
	bindingService *bindingservice.Service
}

func (verifier bindingGuardianVerifier) DeviceBelongsToParent(
	ctx context.Context,
	parentAccountID string,
	deviceID string,
) (bool, error) {
	bindings, err := verifier.bindingService.List(ctx, parentAccountID)
	if err != nil {
		return false, err
	}
	for _, binding := range bindings {
		if strings.TrimSpace(binding.DeviceID) == strings.TrimSpace(deviceID) {
			return true, nil
		}
	}
	return false, nil
}

// DeviceIDsForParent lists every device id bound to one guardian account.
func (verifier bindingGuardianVerifier) DeviceIDsForParent(
	ctx context.Context,
	parentAccountID string,
) ([]string, error) {
	bindings, err := verifier.bindingService.List(ctx, parentAccountID)
	if err != nil {
		return nil, err
	}
	deviceIDs := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		deviceID := strings.TrimSpace(binding.DeviceID)
		if deviceID == "" {
			continue
		}
		deviceIDs = append(deviceIDs, deviceID)
	}
	return deviceIDs, nil
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
