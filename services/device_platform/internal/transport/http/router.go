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
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/buildinfo"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/config"
	gatewaydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/domain"
	gatewayservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ai_gateway/service"
	alarmreminderservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/service"
	assessmentservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/assessment/service"
	auditdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	audithandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/handler"
	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/channel"
	billingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/billing/service"
	bluetoothaudioservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/service"
	careviewservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/care_view/service"
	childservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/child/service"
	companionshipservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/companionship/service"
	connectivityservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/connectivity/service"
	contentservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content/service"
	contentofflinepolicyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/content_offline_policy/service"
	bindingservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_binding/service"
	devicemeshservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_mesh/service"
	runtimeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/device_runtime/service"
	devicechildservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/devicechild/service"
	displayservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/display/service"
	emotionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/emotion/service"
	englishservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/english/service"
	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
	familyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/service"
	featurecenterservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/feature_center/service"
	formfactorservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/form_factor/service"
	growthreportservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/growthreport/service"
	imageprivacyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/image_privacy/service"
	inputservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/input/service"
	intercomservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/intercom/service"
	learningvisualizationservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/learning_visualization/service"
	localcommandservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/local_command/service"
	memoryservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/memory/service"
	motionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/motion/service"
	notificationhandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/handler"
	notificationservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/notification/service"
	operationsdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/domain"
	operationsservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/operations/service"
	otaservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ota/service"
	policeservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/parent_policy/service"
	playbackservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/playback/service"
	powerservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/power/service"
	privacyhandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/handler"
	privacyservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/privacy/service"
	recommendationservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/recommendation/service"
	releasestoreservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/service"
	screenservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/service"
	serviceversionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
	telemetryservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/telemetry/service"
	timelineservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/service"
	ui_textservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/ui_text/service"
	usagereportservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/usage_report/service"
	videoservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_companion/service"
	videosessionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/video_session/service"
	visionhandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/handler"
	visionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/vision/service"
	wearableservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/wearable/service"
)

// RouterOptions contains dependencies for the device platform HTTP transport.
type RouterOptions struct {
	Logger                       *slog.Logger
	BuildVersion                 string
	InternalAPIConfig            config.InternalAPIConfig
	ReadinessProbes              []ReadinessProbe
	AuthService                  *authservice.Service
	ChildService                 *childservice.Service
	FamilyService                *familyservice.Service
	CompanionshipService         *companionshipservice.Service
	ParentPolicyService          *policeservice.Service
	AIService                    *gatewayservice.Service
	OperationsService            *operationsservice.Service
	BindingService               *bindingservice.Service
	RuntimeService               *runtimeservice.Service
	DiagnosticService            auditService
	ServiceVersionService        *serviceversionservice.Service
	TelemetryService             *telemetryservice.Service
	FeatureCenterService         *featurecenterservice.Service
	ReleaseStoreService          *releasestoreservice.Service
	OTAService                   *otaservice.Service
	ContentService               *contentservice.Service
	AIExecutionStore             aiExecutionStore
	AIExecutionTokenTTL          time.Duration
	AIExecutionLeaseTTL          time.Duration
	AIUpstreamURL                string
	AIUpstreamHTTPClient         *http.Client
	ContentOfflinePolicyService  *contentofflinepolicyservice.Service
	UITextService                *ui_textservice.Service
	EnglishService               *englishservice.Service
	PlaybackService              *playbackservice.Service
	LocalCommandService          *localcommandservice.Service
	RecommendationService        *recommendationservice.Service
	UsageReportService           *usagereportservice.Service
	VoiceTokenIssuer             bindingservice.VoiceTokenIssuer
	VoiceWebSocketURL            string
	VoiceConsentReader           voiceConsentReader
	PrivacyService               *privacyservice.Service
	NotificationService          *notificationservice.Service
	VisionService                *visionservice.Service
	DisplayService               *displayservice.Service
	InputService                 *inputservice.Service
	ConnectivityService          *connectivityservice.Service
	PowerService                 *powerservice.Service
	MotionService                *motionservice.Service
	FormFactorService            *formfactorservice.Service
	VideoCompanionService        *videoservice.Service
	VideoSessionService          *videosessionservice.Service
	AlarmReminderService         *alarmreminderservice.Service
	BluetoothAudioService        *bluetoothaudioservice.Service
	LearningVisualizationService *learningvisualizationservice.Service
	ImagePrivacyService          *imageprivacyservice.Service
	WearableService              *wearableservice.Service
	DeviceMeshService            *devicemeshservice.Service
	ScreenService                *screenservice.Service
	MemoryService                *memoryservice.Service
	GrowthReportService          *growthreportservice.Service
	DeviceChildService           *devicechildservice.Service
	// IntercomService enables the guardian↔device voice intercom routes. When it
	// is nil the routes are not registered, so a deployment without intercom
	// answers 404 rather than half-serving a call.
	IntercomService *intercomservice.Service
	// CareViewService serves the guardian live camera view. When it is nil the
	// care-view routes are not registered at all, so a deployment that does not
	// offer live viewing answers 404 instead of exposing half a feature.
	CareViewService *careviewservice.Service
	// AssessmentService serves the learning assessment. When it is nil the
	// assessment routes are not registered at all, so a deployment that does
	// not offer assessments answers 404 instead of half a feature.
	AssessmentService *assessmentservice.Service
	// EmotionService serves mood sensing. When it is nil the routes are not
	// registered at all, so a deployment that does not offer the feature answers
	// 404 instead of exposing half of it.
	EmotionService *emotionservice.Service
	// TimelineService serves the growth timeline. When it is nil the timeline
	// routes are not registered at all, so a deployment that does not assemble
	// one answers 404 instead of serving an empty story that looks like a child
	// with no history.
	TimelineService *timelineservice.Service
	// MemoryChildResolver maps a device's family to its child profile for the
	// memory sync routes. The routes are registered only when it is set, so a
	// deployment that does not wire it keeps answering 404 instead of syncing a
	// guessed child.
	MemoryChildResolver memoryDeviceChildResolver
	AuditRecorder       auditRecorder
	// Billing groups the billing domain services. A nil entry disables only
	// the routes that need it, so the rest of the platform still starts.
	Billing BillingOptions
	// RedisClient backs cross-replica authentication rate limits. When nil,
	// login and verification endpoints fail closed instead of running without
	// brute-force protection.
	RedisClient rateLimitStore
}

// BillingOptions wires the billing domain into the HTTP transport.
//
// Service serves the guardian and operator routes; Channels backs the payment
// provider callback and the operator channel-health view. The callback route is
// registered even when Channels is nil so a misdirected provider call receives
// an explicit "not available" answer instead of a 404 that looks like a
// deployment problem.
type BillingOptions struct {
	Service  *billingservice.Service
	Channels *channel.Registry
}

// ReadinessProbe checks one runtime dependency before the platform accepts
// traffic. Implementations must be safe for concurrent use and should return
// promptly so the route remains a process-level health check.
type ReadinessProbe func(ctx context.Context) error

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

// aiExecutionAuthorizer validates an execution model without returning any
// upstream credential to the transport layer.
type aiExecutionAuthorizer interface {
	AuthorizeExecution(
		ctx context.Context,
		parentAccountID string,
		model string,
	) (*gatewaydomain.Credential, error)
}

// NewRouter returns the HTTP router for the device platform.
func NewRouter(options RouterOptions) http.Handler {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(options.BuildVersion))
	mux.HandleFunc("GET /readyz", readyHandler(options.ReadinessProbes))
	if options.OperationsService != nil {
		mux.HandleFunc(
			"GET /api/v1/public/registration-policy",
			publicRegistrationPolicyHandler(options.OperationsService),
		)
	}

	if options.AuthService != nil {
		authHandler := authHandler{
			service: options.AuthService,
			audit:   options.AuditRecorder,
			limiter: authRateLimiter{store: options.RedisClient},
		}
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
		adminPermissionHandler := adminPermissionHandler{
			service: options.AuthService,
			audit:   options.AuditRecorder,
		}
		mux.HandleFunc(
			"GET /api/v1/admin/permissions",
			authHandler.requireAdminPermission(
				authdomain.AdminPermissionAdminsRead,
				adminPermissionHandler.list,
			),
		)
		mux.HandleFunc(
			"PUT /api/v1/admin/permissions/{account_id}",
			authHandler.requireAdminPermission(
				authdomain.AdminPermissionAdminsManage,
				adminPermissionHandler.update,
			),
		)
		if options.ChildService != nil && options.ParentPolicyService != nil {
			childHandler := childHandler{
				service:       options.ChildService,
				policyService: options.ParentPolicyService,
				consentReader: options.OperationsService,
			}
			childAuthorizer := options.FamilyService
			requireChildRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						childAuthorizer,
						familydomain.ActionChildRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			requireChildManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						childAuthorizer,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/children",
				requireChildRead(childHandler.list),
			)
			mux.HandleFunc(
				"POST /api/v1/children",
				requireChildManage(childHandler.create),
			)
			mux.HandleFunc(
				"PUT /api/v1/children/{child_id}",
				requireChildManage(childHandler.update),
			)
			mux.HandleFunc(
				"DELETE /api/v1/children/{child_id}",
				requireChildManage(childHandler.delete),
			)
			mux.HandleFunc(
				"GET /api/v1/children/{child_id}/policy",
				requireChildRead(childHandler.getPolicy),
			)
			mux.HandleFunc(
				"PUT /api/v1/children/{child_id}/policy",
				requireChildManage(childHandler.updatePolicy),
			)
		}
		if options.FamilyService != nil {
			familyHandler := familyHandler{
				service:       options.FamilyService,
				accountReader: authHandler,
			}
			adminFamilyMembershipHandler := adminFamilyMembershipHandler{
				service: options.FamilyService,
			}
			mux.HandleFunc(
				"GET /api/v1/family/members",
				authHandler.requireAuthentication(
					familyHandler.requireFamilyAction(
						familydomain.ActionMemberRead,
						familyHandler.listMembers,
					),
				),
			)
			mux.HandleFunc(
				"DELETE /api/v1/family/members/{user_id}",
				authHandler.requireAuthentication(familyHandler.removeMember),
			)
			mux.HandleFunc(
				"PUT /api/v1/family/members/{user_id}",
				authHandler.requireAuthentication(
					familyHandler.requireFamilyAction(
						familydomain.ActionMemberManage,
						familyHandler.updateMemberRole,
					),
				),
			)
			mux.HandleFunc(
				"GET /api/v1/family/invitations",
				authHandler.requireAuthentication(
					familyHandler.requireFamilyAction(
						familydomain.ActionInvitationRead,
						familyHandler.listInvitations,
					),
				),
			)
			mux.HandleFunc(
				"GET /api/v1/family/invitations/mine",
				authHandler.requireAuthentication(familyHandler.listMyInvitations),
			)
			mux.HandleFunc(
				"POST /api/v1/family/invitations",
				authHandler.requireAuthentication(
					familyHandler.requireFamilyAction(
						familydomain.ActionInvitationManage,
						familyHandler.createInvitation,
					),
				),
			)
			mux.HandleFunc(
				"DELETE /api/v1/family/invitations/{invitation_id}",
				authHandler.requireAuthentication(
					familyHandler.requireFamilyAction(
						familydomain.ActionInvitationManage,
						familyHandler.revokeInvitation,
					),
				),
			)
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/invitations/{invitation_id}/accept",
				authHandler.requireAuthentication(familyHandler.acceptInvitation),
			)
			mux.HandleFunc(
				"GET /api/v1/family/capability",
				authHandler.requireAuthentication(
					familyHandler.requireFamilyAction(
						familydomain.ActionFamilyRead,
						familyHandler.capability,
					),
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families/{parent_account_id}/members",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyRead,
					adminFamilyMembershipHandler.listMembers,
				),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/families/{parent_account_id}/members/{user_id}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminFamilyMembershipHandler.updateMemberRole,
				),
			)
			mux.HandleFunc(
				"DELETE /api/v1/admin/families/{parent_account_id}/members/{user_id}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminFamilyMembershipHandler.removeMember,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families/{parent_account_id}/invitations",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyRead,
					adminFamilyMembershipHandler.listInvitations,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/families/{parent_account_id}/invitations",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminFamilyMembershipHandler.createInvitation,
				),
			)
			mux.HandleFunc(
				"DELETE /api/v1/admin/families/{parent_account_id}/invitations/{invitation_id}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminFamilyMembershipHandler.revokeInvitation,
				),
			)
		}
		if options.CompanionshipService != nil {
			companionshipHandler := companionshipHandler{
				service: options.CompanionshipService,
			}
			requireCompanionshipRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			requireCompanionshipManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/companionship/growth-records",
				requireCompanionshipRead(companionshipHandler.listGrowthRecords),
			)
			mux.HandleFunc(
				"POST /api/v1/companionship/growth-records",
				requireCompanionshipManage(companionshipHandler.createGrowthRecord),
			)
			mux.HandleFunc(
				"DELETE /api/v1/companionship/growth-records/{record_id}",
				requireCompanionshipManage(companionshipHandler.deleteGrowthRecord),
			)
			mux.HandleFunc(
				"GET /api/v1/companionship/habit-reminders",
				requireCompanionshipRead(companionshipHandler.listHabitReminders),
			)
			mux.HandleFunc(
				"POST /api/v1/companionship/habit-reminders",
				requireCompanionshipManage(companionshipHandler.createHabitReminder),
			)
			mux.HandleFunc(
				"PUT /api/v1/companionship/habit-reminders/{reminder_id}",
				requireCompanionshipManage(companionshipHandler.updateHabitReminder),
			)
			mux.HandleFunc(
				"DELETE /api/v1/companionship/habit-reminders/{reminder_id}",
				requireCompanionshipManage(companionshipHandler.deleteHabitReminder),
			)
		}
		if options.MemoryService != nil {
			memoryEndpoints := memoryHandler{service: options.MemoryService}
			// Reading a memory exposes the child's own statements, so it needs the
			// child read action; writing or deleting one changes the data subject's
			// record and therefore needs the manage action.
			requireMemoryRead := func(next func(
				http.ResponseWriter,
				*http.Request,
				string,
			)) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildRead,
						next,
					),
				)
			}
			requireMemoryManage := func(next func(
				http.ResponseWriter,
				*http.Request,
				string,
			)) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						next,
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/memories",
				requireMemoryRead(memoryEndpoints.overview),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/children/{child_id}/memories",
				requireMemoryRead(memoryEndpoints.list),
			)
			mux.HandleFunc(
				"PUT /api/v1/families/{family_id}/children/{child_id}/memories",
				requireMemoryManage(memoryEndpoints.set),
			)
			mux.HandleFunc(
				"DELETE /api/v1/families/{family_id}/children/{child_id}/memories",
				requireMemoryManage(memoryEndpoints.removeAll),
			)
			mux.HandleFunc(
				"DELETE /api/v1/families/{family_id}/children/{child_id}/memories/{memory_key}",
				requireMemoryManage(memoryEndpoints.remove),
			)
			// Devices sync with a device session token instead of a guardian
			// credential, because the robot has no logged-in guardian at bedtime.
			if options.BindingService != nil && options.MemoryChildResolver != nil {
				deviceMemoryEndpoints := deviceMemoryHandler{
					handler:        options.MemoryService,
					familyResolver: options.RuntimeService,
					childResolver:  options.MemoryChildResolver,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/memories",
					requireDeviceSession(options.BindingService, deviceMemoryEndpoints.syncForDevice),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/memories",
					requireDeviceSession(options.BindingService, deviceMemoryEndpoints.upsertForDevice),
				)
			}
		}
		if options.GrowthReportService != nil {
			guardianReports := guardianHandler{service: options.GrowthReportService}
			requireReportRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/children/{child_id}/growth-reports",
				requireReportRead(guardianReports.list),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/children/{child_id}/growth-reports/{report_id}",
				requireReportRead(guardianReports.get),
			)
			adminReports := adminGrowthReportHandler{service: options.GrowthReportService}
			mux.HandleFunc(
				"GET /api/v1/admin/families/{family_id}/growth-reports",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyRead,
					adminReports.list,
				),
			)
		}
		if options.DeviceChildService != nil {
			guardianBindings := guardianBindingHandler{
				service: options.DeviceChildService,
			}
			// A switch tells the device to reload so the new child's level applies
			// immediately instead of after the next background refresh.
			if options.RuntimeService != nil {
				guardianBindings.commands = options.RuntimeService
			}
			requireBindingManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"PUT /api/v1/families/{family_id}/devices/{device_id}/child",
				requireBindingManage(guardianBindings.activate),
			)
			mux.HandleFunc(
				"DELETE /api/v1/families/{family_id}/devices/{device_id}/child",
				requireBindingManage(guardianBindings.clear),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/devices/{device_id}/child-history",
				requireBindingManage(guardianBindings.history),
			)
			// The device asks with its own session token, because a robot at
			// bedtime has no logged-in guardian to ask on its behalf.
			if options.BindingService != nil {
				deviceChildBindings := deviceChildBindingHandler{
					service: options.DeviceChildService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/child",
					requireDeviceSession(options.BindingService, deviceChildBindings.current),
				)
			}
		}
		if options.CareViewService != nil {
			guardianCareView := guardianCareViewHandler{service: options.CareViewService}
			// Looking into a child's room is the most sensitive action a guardian
			// can take here, so it carries the same family management permission
			// as calling the device.
			requireCareViewManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/devices/{device_id}/care-view-sessions",
				requireCareViewManage(guardianCareView.start),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/care-view-sessions",
				requireCareViewManage(guardianCareView.history),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/care-view-sessions/{session_id}",
				requireCareViewManage(guardianCareView.get),
			)
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/care-view-sessions/{session_id}/end",
				requireCareViewManage(guardianCareView.end),
			)
			// The device answers with its own session token: the child closes the
			// view on the robot, not on a phone.
			if options.BindingService != nil {
				deviceCareView := deviceCareViewHandler{service: options.CareViewService}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/care-view-sessions/{session_id}",
					requireDeviceSession(options.BindingService, deviceCareView.current),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/care-view-sessions/{session_id}/accept",
					requireDeviceSession(options.BindingService, deviceCareView.accept),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/care-view-sessions/{session_id}/decline",
					requireDeviceSession(options.BindingService, deviceCareView.decline),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/care-view-sessions/{session_id}/end",
					requireDeviceSession(options.BindingService, deviceCareView.end),
				)
			}
		}
		if options.AssessmentService != nil {
			guardianAssessment := guardianAssessmentHandler{service: options.AssessmentService}
			// Starting or answering an assessment is a family action on a child's
			// learning data, so it carries the child-management permission.
			requireAssessmentManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/assessment-sessions",
				requireAssessmentManage(guardianAssessment.start),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/assessment-sessions",
				requireAssessmentManage(guardianAssessment.history),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/assessment-sessions/{session_id}",
				requireAssessmentManage(guardianAssessment.get),
			)
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/assessment-sessions/{session_id}/answers",
				requireAssessmentManage(guardianAssessment.answer),
			)
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/assessment-sessions/{session_id}/finish",
				requireAssessmentManage(guardianAssessment.finish),
			)
			// The device reads the assessment it was asked to show and reports the
			// answers it observed, so practice does not need a guardian holding a
			// phone. Starting, finishing, and changing the level stay with the
			// guardian routes.
			if options.BindingService != nil {
				deviceAssessment := deviceAssessmentHandler{service: options.AssessmentService}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/assessment-sessions/{session_id}",
					requireDeviceSession(options.BindingService, deviceAssessment.current),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/assessment-sessions/{session_id}/answers",
					requireDeviceSession(options.BindingService, deviceAssessment.answer),
				)
			}
		}
		if options.TimelineService != nil {
			guardianTimeline := guardianTimelineHandler{service: options.TimelineService}
			// The timeline gathers records about one child, so reading it is the
			// same family action as managing that child's data.
			requireTimelineManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/timeline",
				requireTimelineManage(guardianTimeline.list),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/timeline-milestones",
				requireTimelineManage(guardianTimeline.milestones),
			)
		}
		if options.EmotionService != nil {
			guardianEmotion := guardianEmotionHandler{service: options.EmotionService}
			// The switch decides whether a child's feelings are recorded at all,
			// so changing it carries the child-management permission rather than
			// a lighter one.
			requireEmotionManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/emotion-settings",
				requireEmotionManage(guardianEmotion.settings),
			)
			mux.HandleFunc(
				"PUT /api/v1/families/{family_id}/emotion-settings",
				requireEmotionManage(guardianEmotion.updateSettings),
			)
			// The summary is aggregate-only by construction; there is no route
			// that lists individual observations, and that absence is the feature
			// contract rather than an unfinished endpoint.
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/emotion-summary",
				requireEmotionManage(guardianEmotion.summary),
			)
			// The device reports with its own session token: it is the device
			// looking at the child, not a guardian's phone.
			if options.BindingService != nil {
				deviceEmotion := deviceEmotionHandler{service: options.EmotionService}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/emotion-observations",
					requireDeviceSession(options.BindingService, deviceEmotion.record),
				)
			}
		}
		if options.IntercomService != nil {
			guardianIntercom := guardianIntercomHandler{service: options.IntercomService}
			// Calling a device is a family action, so it requires the guardian to
			// hold the device-management permission for that family.
			requireIntercomManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/devices/{device_id}/intercom-calls",
				requireIntercomManage(guardianIntercom.start),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/intercom-calls",
				requireIntercomManage(guardianIntercom.history),
			)
			mux.HandleFunc(
				"GET /api/v1/families/{family_id}/intercom-calls/{call_id}",
				requireIntercomManage(guardianIntercom.get),
			)
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/intercom-calls/{call_id}/cancel",
				requireIntercomManage(guardianIntercom.cancel),
			)
			mux.HandleFunc(
				"POST /api/v1/families/{family_id}/intercom-calls/{call_id}/hangup",
				requireIntercomManage(guardianIntercom.hangUp),
			)
			// The device answers with its own session token: the child presses a
			// button on the robot, not on a phone.
			if options.BindingService != nil {
				deviceIntercom := deviceIntercomHandler{service: options.IntercomService}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/intercom-calls/{call_id}",
					requireDeviceSession(options.BindingService, deviceIntercom.current),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/intercom-calls/{call_id}/accept",
					requireDeviceSession(options.BindingService, deviceIntercom.accept),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/intercom-calls/{call_id}/decline",
					requireDeviceSession(options.BindingService, deviceIntercom.decline),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/intercom-calls/{call_id}/hangup",
					requireDeviceSession(options.BindingService, deviceIntercom.hangUp),
				)
			}
		}
		if options.AlarmReminderService != nil {
			alarmReminderHandler := alarmReminderHandler{service: options.AlarmReminderService}
			requireAlarmRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			requireAlarmManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/alarm-reminders",
				requireAlarmRead(alarmReminderHandler.listAlarms),
			)
			mux.HandleFunc(
				"POST /api/v1/alarm-reminders",
				requireAlarmManage(alarmReminderHandler.createAlarm),
			)
			mux.HandleFunc(
				"PUT /api/v1/alarm-reminders/{alarm_id}",
				requireAlarmManage(alarmReminderHandler.updateAlarm),
			)
			mux.HandleFunc(
				"DELETE /api/v1/alarm-reminders/{alarm_id}",
				requireAlarmManage(alarmReminderHandler.deleteAlarm),
			)
			mux.HandleFunc(
				"GET /api/v1/alarm-reminders/events",
				requireAlarmRead(alarmReminderHandler.listEvents),
			)
		}
		if options.EnglishService != nil {
			englishHandler := englishHandler{
				service: options.EnglishService,
			}
			requireEnglishRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/english/lessons",
				requireEnglishRead(englishHandler.listLessons),
			)
			mux.HandleFunc(
				"GET /api/v1/english/lessons/{lesson_id}",
				requireEnglishRead(englishHandler.getLesson),
			)
			mux.HandleFunc(
				"GET /api/v1/english/progress",
				requireEnglishRead(englishHandler.listProgress),
			)
			mux.HandleFunc(
				"GET /api/v1/english/attempts",
				requireEnglishRead(englishHandler.listAttempts),
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
					requireDeviceSession(options.BindingService, deviceEnglishHandler.recordAttempt),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/english/lessons",
					requireDeviceSession(options.BindingService, deviceEnglishHandler.listLessons),
				)
			}
		}
		if options.PlaybackService != nil {
			playbackHandler := playbackHandler{
				service: options.PlaybackService,
			}
			requirePlaybackRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			requirePlaybackManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"POST /api/v1/playback/requests",
				requirePlaybackManage(playbackHandler.create),
			)
			mux.HandleFunc(
				"GET /api/v1/playback/requests",
				requirePlaybackRead(playbackHandler.list),
			)
			mux.HandleFunc(
				"GET /api/v1/playback/requests/{request_id}",
				requirePlaybackRead(playbackHandler.get),
			)
			mux.HandleFunc(
				"POST /api/v1/playback/requests/{request_id}/cancel",
				requirePlaybackManage(playbackHandler.cancel),
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
					requireDeviceSession(options.BindingService, devicePlaybackHandler.listPending),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/playback/requests/{request_id}/acknowledge",
					requireDeviceSession(options.BindingService, devicePlaybackHandler.acknowledge),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/playback/requests/{request_id}/complete",
					requireDeviceSession(options.BindingService, devicePlaybackHandler.complete),
				)
			}
		}
		if options.RecommendationService != nil {
			recommendationHandler := recommendationHandler{
				service: options.RecommendationService,
			}
			mux.HandleFunc(
				"GET /api/v1/content/recommendations",
				authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionChildRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							recommendationHandler.list(response, request)
						},
					),
				),
			)
		}
		if options.LocalCommandService != nil {
			localCommandHandler := localCommandHandler{
				service: options.LocalCommandService,
			}
			requireLocalCommandRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			requireLocalCommandManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/local-commands",
				requireLocalCommandRead(localCommandHandler.list),
			)
			mux.HandleFunc(
				"PUT /api/v1/local-commands",
				requireLocalCommandManage(localCommandHandler.replace),
			)
			mux.HandleFunc(
				"DELETE /api/v1/local-commands/{command_id}",
				requireLocalCommandManage(localCommandHandler.delete),
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
					requireDeviceSession(options.BindingService, deviceLocalCommandHandler.listForDevice),
				)
			}
		}
		if options.ContentOfflinePolicyService != nil {
			offlinePolicyHandler := contentOfflinePolicyHandler{
				service: options.ContentOfflinePolicyService,
			}
			requireOfflinePolicyRead := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			requireOfflinePolicyManage := func(next http.HandlerFunc) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/content/offline-policy",
				requireOfflinePolicyRead(offlinePolicyHandler.current),
			)
			mux.HandleFunc(
				"PUT /api/v1/content/offline-policy",
				requireOfflinePolicyManage(offlinePolicyHandler.publish),
			)
			mux.HandleFunc(
				"GET /api/v1/content/offline-policy/acknowledgements",
				requireOfflinePolicyRead(offlinePolicyHandler.acknowledgements),
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
					requireDeviceSession(options.BindingService, deviceOfflinePolicyHandler.currentForDevice),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/content/offline-policy/acknowledgement",
					requireDeviceSession(options.BindingService, deviceOfflinePolicyHandler.acknowledgeForDevice),
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
					requireDeviceSession(options.BindingService, deviceDisplayHandler.reportStatus),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/display/policy",
					requireDeviceSession(options.BindingService, deviceDisplayHandler.devicePolicy),
				)
			}
		}
		if options.ScreenService != nil {
			screenHandler := screenHandler{service: options.ScreenService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceScreenHandler := deviceScreenHandler{
					handler:        &screenHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/screen/status",
					requireDeviceSession(options.BindingService, deviceScreenHandler.reportStatus),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/screen/policy",
					requireDeviceSession(options.BindingService, deviceScreenHandler.devicePolicy),
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
					requireDeviceSession(options.BindingService, deviceInputHandler.reportStatus),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/touch/policy",
					requireDeviceSession(options.BindingService, deviceInputHandler.devicePolicy),
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
					requireDeviceSession(options.BindingService, deviceConnectivityHandler.reportStatus),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/connectivity/policy",
					requireDeviceSession(options.BindingService, deviceConnectivityHandler.devicePolicy),
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
					requireDeviceSession(options.BindingService, devicePowerHandler.reportStatus),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/power/policy",
					requireDeviceSession(options.BindingService, devicePowerHandler.devicePolicy),
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
					requireDeviceSession(options.BindingService, deviceMotionHandler.reportStatus),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/motion/policy",
					requireDeviceSession(options.BindingService, deviceMotionHandler.devicePolicy),
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
					requireDeviceSession(options.BindingService, deviceFormFactorHandler.report),
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
					requireDeviceSession(options.BindingService, deviceVideoHandler.reportStatus),
				)
			}
		}
		if options.VideoSessionService != nil {
			videoSessionHandler := videoSessionHandler{service: options.VideoSessionService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceVideoSessionHandler := deviceVideoSessionHandler{
					handler:        &videoSessionHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/video-session/policy",
					requireDeviceSession(options.BindingService, deviceVideoSessionHandler.devicePolicy),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/video-session/status",
					requireDeviceSession(options.BindingService, deviceVideoSessionHandler.reportStatus),
				)
			}
		}
		if options.AlarmReminderService != nil {
			alarmReminderHandler := alarmReminderHandler{service: options.AlarmReminderService}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceAlarmReminderHandler := deviceAlarmReminderHandler{
					handler:        &alarmReminderHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/alarm-reminders",
					requireDeviceSession(options.BindingService, deviceAlarmReminderHandler.schedule),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/alarm-reminders/{alarm_id}/events",
					requireDeviceSession(options.BindingService, deviceAlarmReminderHandler.reportEvent),
				)
			}
		}
		if options.BluetoothAudioService != nil {
			bluetoothAudioHandler := bluetoothAudioHandler{
				service: options.BluetoothAudioService,
			}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceBluetoothAudioHandler := deviceBluetoothAudioHandler{
					handler:        &bluetoothAudioHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/bluetooth-audio/policy",
					requireDeviceSession(options.BindingService, deviceBluetoothAudioHandler.devicePolicy),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/bluetooth-audio/status",
					requireDeviceSession(options.BindingService, deviceBluetoothAudioHandler.reportStatus),
				)
			}
		}
		if options.LearningVisualizationService != nil {
			learningVisualizationHandler := learningVisualizationHandler{
				service: options.LearningVisualizationService,
			}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceLearningVisualizationHandler := deviceLearningVisualizationHandler{
					handler:        &learningVisualizationHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/learning-visualization/policy",
					requireDeviceSession(options.BindingService, deviceLearningVisualizationHandler.devicePolicy),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/learning-visualization/status",
					requireDeviceSession(options.BindingService, deviceLearningVisualizationHandler.reportStatus),
				)
			}
		}
		if options.ImagePrivacyService != nil {
			imagePrivacyHandler := imagePrivacyHandler{
				service: options.ImagePrivacyService,
			}
			if options.BindingService != nil && options.RuntimeService != nil {
				deviceImagePrivacyHandler := deviceImagePrivacyHandler{
					handler:        &imagePrivacyHandler,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/image-privacy/policy",
					requireDeviceSession(options.BindingService, deviceImagePrivacyHandler.devicePolicy),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/image-privacy/status",
					requireDeviceSession(options.BindingService, deviceImagePrivacyHandler.reportStatus),
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
					requireDeviceSession(options.BindingService, deviceWearableHandler.reportLocation),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/wearable/policy",
					requireDeviceSession(options.BindingService, deviceWearableHandler.devicePolicy),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/wearable/sos",
					requireDeviceSession(options.BindingService, deviceWearableHandler.reportSOS),
				)
			}
		}
		if options.BindingService != nil {
			bindingHandler := deviceBindingHandler{
				service:           options.BindingService,
				voiceTokenIssuer:  options.VoiceTokenIssuer,
				voiceWebSocketURL: strings.TrimSpace(options.VoiceWebSocketURL),
				voiceConsent:      options.VoiceConsentReader,
				familyAccess:      authenticatedFamilyAccess,
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
				authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceBind,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							bindingHandler.bind(response, request)
						},
					),
				),
			)
			mux.HandleFunc(
				"GET /api/v1/devices",
				authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							bindingHandler.list(response, request)
						},
					),
				),
			)
			mux.HandleFunc(
				"DELETE /api/v1/devices/{device_id}",
				authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							bindingHandler.remove(response, request)
						},
					),
				),
			)
		}
		if options.RuntimeService != nil {
			runtimeHandler := deviceRuntimeHandler{
				service:           options.RuntimeService,
				policyService:     options.ParentPolicyService,
				diagnosticService: options.DiagnosticService,
				bindingRevoker:    options.BindingService,
				familyAccess:      authenticatedFamilyAccess,
			}
			mux.HandleFunc(
				"GET /api/v1/devices/status",
				authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							runtimeHandler.listForParent(response, request)
						},
					),
				),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/status",
				authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionDeviceRead,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							runtimeHandler.getForParent(response, request)
						},
					),
				),
			)
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/runtime/heartbeat",
				requireDeviceSession(options.BindingService, runtimeHandler.recordHeartbeat),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/runtime/commands",
				requireDeviceSession(options.BindingService, runtimeHandler.listDeviceCommands),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/runtime/parent-policy",
				requireDeviceSession(options.BindingService, runtimeHandler.getParentPolicy),
			)
			if options.VisionService != nil {
				deviceVisionHandler := deviceVisionHandler{
					service:        options.VisionService,
					bindingService: options.BindingService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/tasks/pending",
					requireDeviceSession(options.BindingService, deviceVisionHandler.listPendingTasks),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks/{task_id}/acknowledge",
					requireDeviceSession(options.BindingService, deviceVisionHandler.acknowledgeTask),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks/{task_id}/status",
					requireDeviceSession(options.BindingService, deviceVisionHandler.reportStatus),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/results",
					requireDeviceSession(options.BindingService, deviceVisionHandler.reportResult),
				)
			}
			if options.UsageReportService != nil {
				usageHandler := usageReportHandler{
					service:        options.UsageReportService,
					runtimeService: options.RuntimeService,
				}
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/runtime/usage",
					requireDeviceSession(options.BindingService, usageHandler.recordDeviceUsage),
				)
			}
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/runtime/commands/{command_id}/ack",
				requireDeviceSession(options.BindingService, runtimeHandler.acknowledgeCommand),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/devices",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionDevicesRead,
					runtimeHandler.listForAdmin,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/devices/{device_id}/commands",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionDevicesManage,
					runtimeHandler.createCommand,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/devices/{device_id}/commands",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionDevicesRead,
					runtimeHandler.listCommands,
				),
			)
			if options.DiagnosticService != nil {
				diagnosticHandler := audithandler.New(options.DiagnosticService)
				mux.HandleFunc(
					"GET /api/v1/admin/devices/{device_id}/diagnostics",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionDevicesRead,
						diagnosticHandler.GetDiagnostics,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/devices/{device_id}/diagnostics/summary",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionDevicesRead,
						diagnosticHandler.GetSummary,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/devices/{device_id}/provisioning",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionDevicesRead,
						diagnosticHandler.GetProvisioning,
					),
				)
			}
			mux.HandleFunc(
				"POST /api/v1/admin/devices/{device_id}/sessions/revoke",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionDevicesManage,
					runtimeHandler.revokeSessions,
				),
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
				authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						familydomain.ActionAIAccountManage,
						func(
							response http.ResponseWriter,
							request *http.Request,
							ownerAccountID string,
						) {
							adminHandler.updateParentAIModelsAuthorized(
								response,
								request,
								ownerAccountID,
							)
						},
					),
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyRead,
					adminHandler.listFamilies,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/families",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminHandler.createParent,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/families/{parent_account_id}/ai-account",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionAIAccountsManage,
					adminHandler.retryParentAIAccount,
				),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/families/{parent_account_id}/profile",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminHandler.updateParentProfile,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/families/{parent_account_id}/password",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminHandler.resetParentPassword,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families/{parent_account_id}/devices",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyRead,
					adminHandler.listParentDevices,
				),
			)
			mux.HandleFunc(
				"DELETE /api/v1/admin/families/{parent_account_id}/devices/{device_id}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyManage,
					adminHandler.unbindParentDevice,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families/{parent_account_id}/children",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionFamilyRead,
					adminHandler.listFamilyChildren,
				),
			)
			if options.UsageReportService != nil {
				usageHandler := usageReportHandler{service: options.UsageReportService}
				mux.HandleFunc(
					"GET /api/v1/admin/families/{parent_account_id}/usage-reports",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionFamilyRead,
						usageHandler.listAdminFamilyUsageReports,
					),
				)
			}
			mux.HandleFunc(
				"GET /api/v1/admin/ai-accounts",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionAIAccountsRead,
					adminHandler.listAIAccounts,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/ai-account-defaults",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionAIAccountsRead,
					adminHandler.getAIAccountDefaults,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/ai-models",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionAIAccountsRead,
					adminHandler.listAIModels,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/ai-gateway/child-policy",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionAIAccountsRead,
					adminHandler.getAIGatewayChildPolicy,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/families/{parent_account_id}/ai-usage",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionAIAccountsRead,
					adminHandler.getFamilyAIUsage,
				),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/ai-accounts/{provider_account_id}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionAIAccountsManage,
					adminHandler.updateAIAccount,
				),
			)
			if options.OperationsService != nil {
				mux.HandleFunc(
					"GET /api/v1/admin/overview",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						adminHandler.getOverview,
					),
				)
				if options.TelemetryService != nil {
					telemetryHandler := telemetryHandler{
						service: options.TelemetryService,
					}
					mux.HandleFunc(
						"GET /api/v1/admin/telemetry/summary",
						authHandler.requireAdminPermission(
							authdomain.AdminPermissionStatsRead,
							telemetryHandler.summary,
						),
					)
				}
				mux.HandleFunc(
					"GET /api/v1/admin/settings",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionSettingsRead,
						adminHandler.getSettings,
					),
				)
				mux.HandleFunc(
					"PUT /api/v1/admin/settings",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionSettingsManage,
						"admin.settings.updated",
						adminHandler.updateSettings,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/releases",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageRead,
						adminHandler.listReleases,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/releases",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionStorageManage,
						"admin.release.created",
						adminHandler.createRelease,
					),
				)
				mux.HandleFunc(
					"PUT /api/v1/admin/releases/{release_id}/publish",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionStorageManage,
						"admin.release.published",
						adminHandler.publishRelease,
					),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/releases/{release_id}",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionStorageManage,
						"admin.release.deleted",
						adminHandler.deleteRelease,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/release-artifacts/{version}",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageRead,
						adminHandler.getReleaseArtifact,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/app/update",
					adminHandler.appUpdate,
				)
			}
			if options.ReleaseStoreService != nil {
				mux.HandleFunc(
					"GET /api/v1/admin/storage/files",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageRead,
						adminHandler.listReleaseFiles,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/storage/files/digest",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageRead,
						adminHandler.getReleaseFileDigest,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/storage/files/upload",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageManage,
						adminHandler.uploadReleaseFile,
					),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/storage/files/{path...}",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageManage,
						adminHandler.deleteReleaseFile,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/storage/index/status",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageRead,
						adminHandler.getReleaseIndexStatus,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/storage/index/refresh",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageManage,
						adminHandler.refreshReleaseIndex,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/release-files",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageRead,
						adminHandler.listReleaseFiles,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/release-files",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageManage,
						adminHandler.uploadReleaseFile,
					),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/release-files",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageManage,
						adminHandler.deleteReleaseFile,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/release-index/refresh",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStorageManage,
						adminHandler.refreshReleaseIndex,
					),
				)
			}
			if options.ServiceVersionService != nil {
				mux.HandleFunc(
					"GET /api/v1/admin/service-versions",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionServiceRead,
						adminHandler.listServiceVersions,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/service-versions/check",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionServiceRead,
						adminHandler.checkServiceVersions,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/service-versions/upgrade",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionServiceUpgrade,
						"admin.service.upgrade.started",
						adminHandler.upgradeAllServices,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/service-versions/{service}/upgrade",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionServiceUpgrade,
						"admin.service.upgrade.started",
						adminHandler.upgradeService,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/service-versions/{service}/releases",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionServiceRead,
						adminHandler.listServiceReleases,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/service-version-operations",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionServiceRead,
						adminHandler.listServiceVersionOperations,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/service-version-operations/{operation_id}",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionServiceRead,
						adminHandler.getServiceVersionOperation,
					),
				)
			}
			if options.FeatureCenterService != nil {
				featureCenterHandler := NewFeatureCenterHandler(options.FeatureCenterService)
				featureCenterHandler.RegisterAdminRoutes(
					mux,
					authHandler.requireAdminPermissionAudited(
						featureCenterPermission,
						featureCenterAuditAction,
					),
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
				return parentScopedFor(
					options.FamilyService,
					familydomain.ActionFamilyRead,
					action,
				)
			}
			privacyMutationScoped := func(
				action func(
					http.ResponseWriter,
					*http.Request,
					string,
				),
			) http.HandlerFunc {
				return parentScopedFor(
					options.FamilyService,
					familydomain.ActionPrivacyManage,
					action,
				)
			}
			// familyScopedFor wraps a guardian route with the exact family action
			// it requires. Handlers below read the resolved owner from the request
			// context, so this also applies to invited guardians and viewers.
			familyScopedFor := func(
				action familydomain.Action,
				next http.HandlerFunc,
			) http.HandlerFunc {
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						action,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
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
						privacyMutationScoped(privacyHandler.RequestDeletion),
					),
				)
				mux.HandleFunc(
					"DELETE /api/v1/privacy/deletion",
					authHandler.requireAuthentication(
						privacyMutationScoped(privacyHandler.CancelDeletion),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/privacy/consent/withdraw",
					authHandler.requireAuthentication(
						privacyMutationScoped(privacyHandler.WithdrawConsent),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/privacy/consent/grant",
					authHandler.requireAuthentication(
						privacyMutationScoped(privacyHandler.GrantConsent),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/audit",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionAuditRead,
						privacyHandler.AdminAudit,
					),
				)
			}
			if options.VisionService != nil {
				visionHandler := visionhandler.New(options.VisionService)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/capability",
					authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							familydomain.ActionVisionRead,
							visionHandler.GetCapability,
						),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks",
					authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							familydomain.ActionVisionExecute,
							visionHandler.CreateTask,
						),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/tasks",
					authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							familydomain.ActionVisionRead,
							visionHandler.ListTasks,
						),
					),
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/vision/tasks/{task_id}/cancel",
					authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							familydomain.ActionVisionExecute,
							visionHandler.CancelTask,
						),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/results",
					authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							familydomain.ActionVisionRead,
							visionHandler.ListResults,
						),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/vision/results/{task_id}",
					authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							familydomain.ActionVisionRead,
							visionHandler.GetResult,
						),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/vision/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						visionHandler.GetAdminStats,
					),
				)
			}
			if options.DisplayService != nil {
				displayHandler := displayHandler{service: options.DisplayService}
				mux.HandleFunc(
					"GET /api/v1/display/policy",
					familyScopedFor(familydomain.ActionDeviceRead, displayHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/display/policy",
					familyScopedFor(familydomain.ActionDeviceManage, displayHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/display/devices",
					familyScopedFor(familydomain.ActionDeviceRead, displayHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/display/capability",
					familyScopedFor(familydomain.ActionDeviceRead, displayHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/display/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						displayHandler.adminStats,
					),
				)
			}
			if options.ScreenService != nil {
				screenHandler := screenHandler{service: options.ScreenService}
				mux.HandleFunc(
					"GET /api/v1/screen/policy",
					familyScopedFor(familydomain.ActionDeviceRead, screenHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/screen/policy",
					familyScopedFor(familydomain.ActionDeviceManage, screenHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/screen/devices",
					familyScopedFor(familydomain.ActionDeviceRead, screenHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/screen/point-read",
					familyScopedFor(familydomain.ActionDeviceRead, screenHandler.dictionary),
				)
				mux.HandleFunc(
					"PUT /api/v1/screen/point-read",
					familyScopedFor(familydomain.ActionDeviceManage, screenHandler.putDictionaryEntry),
				)
				mux.HandleFunc(
					"DELETE /api/v1/screen/point-read/{entry_id}",
					familyScopedFor(familydomain.ActionDeviceManage, screenHandler.deleteDictionaryEntry),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/screen/capability",
					familyScopedFor(familydomain.ActionDeviceRead, screenHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/screen/playback",
					familyScopedFor(familydomain.ActionDeviceRead, screenHandler.playback),
				)
				mux.HandleFunc(
					"PUT /api/v1/devices/{device_id}/screen/playback",
					familyScopedFor(familydomain.ActionDeviceManage, screenHandler.resolvePlayback),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/screen/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						screenHandler.adminStats,
					),
				)
			}
			if options.VideoSessionService != nil {
				videoSessionHandler := videoSessionHandler{service: options.VideoSessionService}
				mux.HandleFunc(
					"GET /api/v1/video-session/policy",
					familyScopedFor(familydomain.ActionDeviceRead, videoSessionHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/video-session/policy",
					familyScopedFor(familydomain.ActionDeviceManage, videoSessionHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/video-session/devices",
					familyScopedFor(familydomain.ActionDeviceRead, videoSessionHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/video-session/capability",
					familyScopedFor(familydomain.ActionDeviceRead, videoSessionHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/video-session/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						videoSessionHandler.adminStats,
					),
				)
			}
			if options.BluetoothAudioService != nil {
				bluetoothAudioHandler := bluetoothAudioHandler{
					service: options.BluetoothAudioService,
				}
				mux.HandleFunc(
					"GET /api/v1/bluetooth-audio/policy",
					familyScopedFor(familydomain.ActionDeviceRead, bluetoothAudioHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/bluetooth-audio/policy",
					familyScopedFor(familydomain.ActionDeviceManage, bluetoothAudioHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/bluetooth-audio/devices",
					familyScopedFor(familydomain.ActionDeviceRead, bluetoothAudioHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/bluetooth-audio/capability",
					familyScopedFor(familydomain.ActionDeviceRead, bluetoothAudioHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/bluetooth-audio/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						bluetoothAudioHandler.adminStats,
					),
				)
			}
			if options.LearningVisualizationService != nil {
				learningVisualizationHandler := learningVisualizationHandler{
					service: options.LearningVisualizationService,
				}
				mux.HandleFunc(
					"GET /api/v1/learning-visualization/policy",
					familyScopedFor(familydomain.ActionChildRead, learningVisualizationHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/learning-visualization/policy",
					familyScopedFor(familydomain.ActionChildManage, learningVisualizationHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/learning-visualization/devices",
					familyScopedFor(familydomain.ActionChildRead, learningVisualizationHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/learning-visualization/capability",
					familyScopedFor(familydomain.ActionChildRead, learningVisualizationHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/learning-visualization/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						learningVisualizationHandler.adminStats,
					),
				)
			}
			if options.ImagePrivacyService != nil {
				imagePrivacyHandler := imagePrivacyHandler{
					service: options.ImagePrivacyService,
				}
				mux.HandleFunc(
					"GET /api/v1/image-privacy/policy",
					familyScopedFor(familydomain.ActionPrivacyRead, imagePrivacyHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/image-privacy/policy",
					familyScopedFor(familydomain.ActionPrivacyManage, imagePrivacyHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/image-privacy/devices",
					familyScopedFor(familydomain.ActionPrivacyRead, imagePrivacyHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/image-privacy/capability",
					familyScopedFor(familydomain.ActionPrivacyRead, imagePrivacyHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/image-privacy/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						imagePrivacyHandler.adminStats,
					),
				)
			}
			if options.InputService != nil {
				inputHandler := inputHandler{service: options.InputService}
				mux.HandleFunc(
					"GET /api/v1/touch/config",
					familyScopedFor(familydomain.ActionDeviceRead, inputHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/touch/config",
					familyScopedFor(familydomain.ActionDeviceManage, inputHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/touch/devices",
					familyScopedFor(familydomain.ActionDeviceRead, inputHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/touch/capability",
					familyScopedFor(familydomain.ActionDeviceRead, inputHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/touch/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						inputHandler.adminStats,
					),
				)
			}
			if options.ConnectivityService != nil {
				connectivityHandler := connectivityHandler{service: options.ConnectivityService}
				mux.HandleFunc(
					"GET /api/v1/connectivity/policy",
					familyScopedFor(familydomain.ActionDeviceRead, connectivityHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/connectivity/policy",
					familyScopedFor(familydomain.ActionDeviceManage, connectivityHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/connectivity/devices",
					familyScopedFor(familydomain.ActionDeviceRead, connectivityHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/connectivity/capability",
					familyScopedFor(familydomain.ActionDeviceRead, connectivityHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/connectivity/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						connectivityHandler.adminStats,
					),
				)
			}
			if options.PowerService != nil {
				powerHandler := powerHandler{service: options.PowerService}
				mux.HandleFunc(
					"GET /api/v1/power/policy",
					familyScopedFor(familydomain.ActionDeviceRead, powerHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/power/policy",
					familyScopedFor(familydomain.ActionDeviceManage, powerHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/power/devices",
					familyScopedFor(familydomain.ActionDeviceRead, powerHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/power/capability",
					familyScopedFor(familydomain.ActionDeviceRead, powerHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/power/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						powerHandler.adminStats,
					),
				)
			}
			if options.MotionService != nil {
				motionHandler := motionHandler{service: options.MotionService}
				mux.HandleFunc(
					"GET /api/v1/motion/policy",
					familyScopedFor(familydomain.ActionDeviceRead, motionHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/motion/policy",
					familyScopedFor(familydomain.ActionDeviceManage, motionHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/motion/devices",
					familyScopedFor(familydomain.ActionDeviceRead, motionHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/motion/capability",
					familyScopedFor(familydomain.ActionDeviceRead, motionHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/motion/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						motionHandler.adminStats,
					),
				)
			}
			if options.FormFactorService != nil {
				formFactorHandler := formFactorHandler{service: options.FormFactorService}
				mux.HandleFunc(
					"GET /api/v1/form-factor/devices",
					familyScopedFor(familydomain.ActionDeviceRead, formFactorHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/form-factor/capability",
					familyScopedFor(familydomain.ActionDeviceRead, formFactorHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/form-factor/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						formFactorHandler.adminStats,
					),
				)
			}
			if options.VideoCompanionService != nil {
				videoHandler := videoCompanionHandler{service: options.VideoCompanionService}
				mux.HandleFunc(
					"GET /api/v1/video-companion/contacts",
					familyScopedFor(familydomain.ActionDeviceRead, videoHandler.listContacts),
				)
				mux.HandleFunc(
					"POST /api/v1/video-companion/contacts",
					familyScopedFor(familydomain.ActionDeviceManage, videoHandler.createContact),
				)
				mux.HandleFunc(
					"PUT /api/v1/video-companion/contacts/{contact_id}",
					familyScopedFor(familydomain.ActionDeviceManage, videoHandler.updateContact),
				)
				mux.HandleFunc(
					"DELETE /api/v1/video-companion/contacts/{contact_id}",
					familyScopedFor(familydomain.ActionDeviceManage, videoHandler.deleteContact),
				)
				mux.HandleFunc(
					"GET /api/v1/video-companion/calls",
					familyScopedFor(familydomain.ActionDeviceRead, videoHandler.history),
				)
				mux.HandleFunc(
					"POST /api/v1/video-companion/calls",
					familyScopedFor(familydomain.ActionDeviceManage, videoHandler.startCall),
				)
				mux.HandleFunc(
					"PUT /api/v1/video-companion/calls/{call_id}",
					familyScopedFor(familydomain.ActionDeviceManage, videoHandler.updateCall),
				)
				mux.HandleFunc(
					"GET /api/v1/video-companion/devices",
					familyScopedFor(familydomain.ActionDeviceRead, videoHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/video-companion/capability",
					familyScopedFor(familydomain.ActionDeviceRead, videoHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/video-companion/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						videoHandler.adminStats,
					),
				)
			}
			if options.WearableService != nil {
				wearableHandler := wearableHandler{service: options.WearableService}
				mux.HandleFunc(
					"GET /api/v1/wearable/policy",
					familyScopedFor(familydomain.ActionPrivacyRead, wearableHandler.policy),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/policy",
					familyScopedFor(familydomain.ActionPrivacyManage, wearableHandler.updatePolicy),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/contacts",
					familyScopedFor(familydomain.ActionPrivacyRead, wearableHandler.listContacts),
				)
				mux.HandleFunc(
					"POST /api/v1/wearable/contacts",
					familyScopedFor(familydomain.ActionPrivacyManage, wearableHandler.createContact),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/contacts/{contact_id}",
					familyScopedFor(familydomain.ActionPrivacyManage, wearableHandler.updateContact),
				)
				mux.HandleFunc(
					"DELETE /api/v1/wearable/contacts/{contact_id}",
					familyScopedFor(familydomain.ActionPrivacyManage, wearableHandler.deleteContact),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/geofence",
					familyScopedFor(familydomain.ActionPrivacyRead, wearableHandler.geofence),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/geofence",
					familyScopedFor(familydomain.ActionPrivacyManage, wearableHandler.updateGeofence),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/devices",
					familyScopedFor(familydomain.ActionDeviceRead, wearableHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/wearable/capability",
					familyScopedFor(familydomain.ActionDeviceRead, wearableHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/sos",
					familyScopedFor(familydomain.ActionPrivacyRead, wearableHandler.sosHistory),
				)
				mux.HandleFunc(
					"GET /api/v1/wearable/sos/active",
					familyScopedFor(familydomain.ActionPrivacyRead, wearableHandler.activeSOS),
				)
				mux.HandleFunc(
					"PUT /api/v1/wearable/sos/{event_id}/cancel",
					familyScopedFor(familydomain.ActionPrivacyManage, wearableHandler.cancelSOS),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/wearable/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						wearableHandler.adminStats,
					),
				)
			}
			if options.DeviceMeshService != nil {
				meshHandler := deviceMeshHandler{service: options.DeviceMeshService}
				mux.HandleFunc(
					"GET /api/v1/device-mesh/groups",
					familyScopedFor(familydomain.ActionDeviceRead, meshHandler.listGroups),
				)
				mux.HandleFunc(
					"POST /api/v1/device-mesh/groups",
					familyScopedFor(familydomain.ActionDeviceManage, meshHandler.createGroup),
				)
				mux.HandleFunc(
					"DELETE /api/v1/device-mesh/groups/{group_id}",
					familyScopedFor(familydomain.ActionDeviceManage, meshHandler.deleteGroup),
				)
				mux.HandleFunc(
					"GET /api/v1/device-mesh/groups/{group_id}/members",
					familyScopedFor(familydomain.ActionDeviceRead, meshHandler.listMembers),
				)
				mux.HandleFunc(
					"POST /api/v1/device-mesh/groups/{group_id}/members",
					familyScopedFor(familydomain.ActionDeviceManage, meshHandler.addMember),
				)
				mux.HandleFunc(
					"DELETE /api/v1/device-mesh/groups/{group_id}/members/{device_id}",
					familyScopedFor(familydomain.ActionDeviceManage, meshHandler.removeMember),
				)
				mux.HandleFunc(
					"GET /api/v1/device-mesh/messages",
					familyScopedFor(familydomain.ActionDeviceRead, meshHandler.listMessages),
				)
				mux.HandleFunc(
					"POST /api/v1/device-mesh/messages",
					familyScopedFor(familydomain.ActionDeviceManage, meshHandler.routeMessage),
				)
				mux.HandleFunc(
					"GET /api/v1/device-mesh/content",
					familyScopedFor(familydomain.ActionDeviceRead, meshHandler.listContentStates),
				)
				mux.HandleFunc(
					"PUT /api/v1/device-mesh/content",
					familyScopedFor(familydomain.ActionDeviceManage, meshHandler.reportSync),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/device-mesh/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						meshHandler.adminStats,
					),
				)
			}
			if options.NotificationService != nil {
				notificationHandler := notificationhandler.New(options.NotificationService)
				adminNotificationHandler := notificationhandler.NewAdmin(options.NotificationService)
				requireNotificationAction := func(
					action familydomain.Action,
					next func(
						http.ResponseWriter,
						*http.Request,
						string,
					),
				) http.HandlerFunc {
					return authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							action,
							func(
								response http.ResponseWriter,
								request *http.Request,
								ownerAccountID string,
							) {
								next(response, request, ownerAccountID)
							},
						),
					)
				}
				mux.HandleFunc(
					"GET /api/v1/notifications",
					requireNotificationAction(
						familydomain.ActionFamilyRead,
						notificationHandler.Inbox,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/notifications/unread-count",
					requireNotificationAction(
						familydomain.ActionFamilyRead,
						notificationHandler.UnreadCount,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/notifications/read-all",
					requireNotificationAction(
						familydomain.ActionFamilyRead,
						notificationHandler.MarkAllRead,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/notifications/{notification_id}/read",
					requireNotificationAction(
						familydomain.ActionFamilyRead,
						notificationHandler.MarkRead,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/notifications/family-messages",
					requireNotificationAction(
						familydomain.ActionChildManage,
						notificationHandler.SendFamilyMessage,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/notifications/devices/{device_id}/messages",
					requireNotificationAction(
						familydomain.ActionDeviceRead,
						notificationHandler.DeviceMessages,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/notifications",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionNotificationsRead,
						adminNotificationHandler.AdminList,
					),
				)
				mux.HandleFunc(
					"POST /api/v1/admin/notifications",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionNotificationsWrite,
						"admin.notification.created",
						adminScoped(adminNotificationHandler.AdminCreate),
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/notifications/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionNotificationsRead,
						adminNotificationHandler.AdminStats,
					),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/notifications/{notification_id}",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionNotificationsRead,
						adminNotificationHandler.AdminGet,
					),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/notifications/{notification_id}",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionNotificationsWrite,
						"admin.notification.deleted",
						adminScoped(adminNotificationHandler.AdminDelete),
					),
				)
			}
		}
		if options.ContentService != nil {
			contentHandler := contentHandler{service: options.ContentService}
			mux.HandleFunc(
				"GET /api/v1/admin/content/packages",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					contentHandler.listPackages,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentReview,
					contentHandler.createPackage,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/packages/{package_id}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					contentHandler.getPackage,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentReview,
					contentHandler.createPackageVersion,
				),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/content/packages/{package_id}/versions/{package_version}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentReview,
					contentHandler.updatePackage,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/submit",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentReview,
					contentHandler.submitPackage,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/approve",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.reviewed",
					contentHandler.approvePackage,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/reject",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.reviewed",
					contentHandler.rejectPackage,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/publish",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.published",
					contentHandler.publishPackage,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/withdraw",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.withdrawn",
					contentHandler.withdrawPackage,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/packages/{package_id}/versions/{package_version}/archive",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentReview,
					contentHandler.archivePackage,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/theme-packs",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					contentHandler.listThemePacks,
				),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/content/theme-packs/{theme_id}",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.theme_pack.updated",
					contentHandler.upsertThemePack,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/theme-packs/{theme_id}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					contentHandler.getThemePack,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/theme-packs/{theme_id}/publish",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.theme_pack.published",
					contentHandler.publishThemePack,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/content/theme-packs/{theme_id}/withdraw",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.theme_pack.withdrawn",
					contentHandler.withdrawThemePack,
				),
			)
			mux.HandleFunc(
				"DELETE /api/v1/admin/content/theme-packs/{theme_id}",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.theme_pack.deleted",
					contentHandler.deleteThemePack,
				),
			)
		}
		if options.ContentOfflinePolicyService != nil {
			adminOfflinePolicyHandler := contentOfflinePolicyHandler{
				service: options.ContentOfflinePolicyService,
			}
			mux.HandleFunc(
				"GET /api/v1/admin/content/offline-policy",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					adminOfflinePolicyHandler.adminCurrent,
				),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/content/offline-policy",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.content.offline_policy.published",
					adminOfflinePolicyHandler.adminPublish,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/content/offline-policy/acknowledgements",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					adminOfflinePolicyHandler.acknowledgementsForAdmin,
				),
			)
		}
		if options.UITextService != nil {
			uiTextAdminHandler := uiTextHandler{service: options.UITextService}
			mux.HandleFunc(
				"GET /api/v1/admin/ui-text/packages",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					uiTextAdminHandler.listVersions,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/ui-text/packages/drafts",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.ui_text.draft.created",
					uiTextAdminHandler.createDraft,
				),
			)
			mux.HandleFunc(
				"GET /api/v1/admin/ui-text/packages/{package_id}/{language}/{platform}/{package_version}",
				authHandler.requireAdminPermission(
					authdomain.AdminPermissionContentRead,
					uiTextAdminHandler.getVersion,
				),
			)
			mux.HandleFunc(
				"PUT /api/v1/admin/ui-text/packages/{package_id}/{language}/{platform}/{package_version}",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.ui_text.draft.updated",
					uiTextAdminHandler.updateDraft,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/ui-text/packages/{package_id}/{language}/{platform}/{package_version}/submit",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.ui_text.submit",
					uiTextAdminHandler.submit,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/ui-text/packages/{package_id}/{language}/{platform}/{package_version}/approve",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.ui_text.approve",
					uiTextAdminHandler.approve,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/ui-text/packages/{package_id}/{language}/{platform}/{package_version}/reject",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.ui_text.review.rejected",
					uiTextAdminHandler.reject,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/ui-text/packages/{package_id}/{language}/{platform}/{package_version}/publish",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.ui_text.published",
					uiTextAdminHandler.publish,
				),
			)
			mux.HandleFunc(
				"POST /api/v1/admin/ui-text/packages/{package_id}/{language}/{platform}/{package_version}/withdraw",
				authHandler.requireAdminPermissionAction(
					authdomain.AdminPermissionContentReview,
					"admin.ui_text.withdraw",
					uiTextAdminHandler.withdraw,
				),
			)
		}
	}

	if options.UsageReportService != nil && options.AuthService != nil {
		usageHandler := usageReportHandler{service: options.UsageReportService}
		authHandler := authHandler{service: options.AuthService}
		mux.HandleFunc(
			"GET /api/v1/usage-reports",
			authHandler.requireAuthentication(
				parentScopedFor(
					options.FamilyService,
					familydomain.ActionDeviceRead,
					func(
						response http.ResponseWriter,
						request *http.Request,
						_ string,
					) {
						usageHandler.listGuardianUsageReports(response, request)
					},
				),
			),
		)
	}

	if options.ContentService != nil {
		contentHandler := contentHandler{
			service:         options.ContentService,
			guardianDevices: guardianDeviceListerFromBinding(options.BindingService),
		}
		contentFamilyScopedFor := func(
			action familydomain.Action,
		) func(http.HandlerFunc) http.HandlerFunc {
			return func(next http.HandlerFunc) http.HandlerFunc {
				if options.AuthService == nil {
					return next
				}
				authHandler := authHandler{service: options.AuthService}
				return authHandler.requireAuthentication(
					parentScopedFor(
						options.FamilyService,
						action,
						func(
							response http.ResponseWriter,
							request *http.Request,
							_ string,
						) {
							next(response, request)
						},
					),
				)
			}
		}
		requireContentChildRead := contentFamilyScopedFor(familydomain.ActionChildRead)
		requireContentChildManage := contentFamilyScopedFor(familydomain.ActionChildManage)
		requireContentDeviceRead := contentFamilyScopedFor(familydomain.ActionDeviceRead)
		// The parent app reads the catalog with a guardian session.
		if options.AuthService != nil {
			mux.HandleFunc(
				"GET /api/v1/content/catalog",
				requireContentChildRead(contentHandler.catalog),
			)
			mux.HandleFunc(
				"GET /api/v1/content/packages/{package_id}/download",
				requireContentChildRead(contentHandler.download),
			)
			mux.HandleFunc(
				"GET /api/v1/content/favorites",
				requireContentChildRead(contentHandler.listFavorites),
			)
			mux.HandleFunc(
				"PUT /api/v1/content/favorites/{package_id}",
				requireContentChildManage(contentHandler.addFavorite),
			)
			mux.HandleFunc(
				"DELETE /api/v1/content/favorites/{package_id}",
				requireContentChildManage(contentHandler.removeFavorite),
			)
			mux.HandleFunc(
				"GET /api/v1/content/play-history",
				requireContentDeviceRead(contentHandler.listPlayHistoryForGuardian),
			)
			mux.HandleFunc(
				"GET /api/v1/content/theme-packs",
				requireContentChildRead(contentHandler.listPublishedThemePacks),
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
				requireDeviceSession(options.BindingService, deviceHandler.catalog),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/content/packages/{package_id}/download",
				requireDeviceSession(options.BindingService, deviceHandler.download),
			)
			mux.HandleFunc(
				"POST /api/v1/devices/{device_id}/content/playback",
				requireDeviceSession(options.BindingService, deviceHandler.recordPlayback),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/content/playback",
				requireDeviceSession(options.BindingService, deviceHandler.listPlayHistory),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/content/favorites",
				requireDeviceSession(options.BindingService, deviceHandler.listFavorites),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/content/theme-packs",
				requireDeviceSession(options.BindingService, deviceHandler.listThemePacks),
			)
		}
	}

	if options.UITextService != nil {
		uiTextHandler := uiTextHandler{service: options.UITextService}
		// The parent app and console pull published copy with a session so a
		// client only ever renders text the platform has published.
		if options.AuthService != nil {
			authHandler := authHandler{service: options.AuthService}
			mux.HandleFunc(
				"GET /api/v1/ui-text/packages",
				authHandler.requireAuthentication(uiTextHandler.published),
			)
			mux.HandleFunc(
				"GET /api/v1/ui-text/packages/{package_id}/{language}/{platform}",
				authHandler.requireAuthentication(uiTextHandler.publishedPackage),
			)
		}
		// Firmware devices hold a device session token instead of a guardian
		// credential, so the same published copy is exposed behind a verified
		// session keyed to the path device id.
		if options.BindingService != nil {
			deviceHandler := deviceUITextHandler{
				handler:        uiTextHandler,
				bindingService: options.BindingService,
			}
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/ui-text/packages",
				requireDeviceSession(options.BindingService, deviceHandler.published),
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/ui-text/packages/{package_id}/{language}/{platform}",
				requireDeviceSession(options.BindingService, deviceHandler.publishedPackage),
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
		if options.BindingService != nil &&
			options.AIService != nil &&
			options.AIExecutionStore != nil {
			internalHandler := internalHandler{
				bindingService:       options.BindingService,
				aiService:            options.AIService,
				executionStore:       options.AIExecutionStore,
				executionTokenTTL:    options.AIExecutionTokenTTL,
				executionLeaseTTL:    options.AIExecutionLeaseTTL,
				aiUpstreamURL:        options.AIUpstreamURL,
				aiHTTPClient:         options.AIUpstreamHTTPClient,
				requireHTTPSUpstream: true,
			}
			mux.Handle(
				"GET /internal/v1/devices/{device_id}/ai-execution-ticket",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(internalHandler.aiExecutionTicket),
				),
			)
			if options.CareViewService != nil {
				// The voice gateway re-reads session state here before relaying a
				// single frame, so it never trusts the state carried by an earlier
				// authorize call. Service-token protected: only the relay may ask.
				internalCareView := internalCareViewHandler{service: options.CareViewService}
				mux.Handle(
					"GET /internal/v1/devices/{device_id}/care-view-sessions/{session_id}",
					requireServiceToken(
						options.InternalAPIConfig.AuthToken,
						http.HandlerFunc(internalCareView.session),
					),
				)
			}
			if options.IntercomService != nil {
				// The voice gateway re-reads call state here before relaying
				// audio. It is service-token protected because only the gateway
				// may ask, and it answers for the owning device only.
				internalIntercom := internalIntercomHandler{service: options.IntercomService}
				mux.Handle(
					"GET /internal/v1/devices/{device_id}/intercom-calls/{call_id}",
					requireServiceToken(
						options.InternalAPIConfig.AuthToken,
						http.HandlerFunc(internalIntercom.session),
					),
				)
			}
			mux.Handle(
				"POST /internal/v1/ai-execution",
				requireServiceToken(
					options.InternalAPIConfig.AuthToken,
					http.HandlerFunc(internalHandler.aiExecution),
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
		var requireAdmin func(http.HandlerFunc) http.HandlerFunc
		var requireOTADeviceRead func(http.HandlerFunc) http.HandlerFunc
		var requireOTADeviceManage func(http.HandlerFunc) http.HandlerFunc
		if options.AuthService != nil {
			authHandler := authHandler{
				service: options.AuthService,
				audit:   options.AuditRecorder,
			}
			requireAdmin = authHandler.requireAdminPermissionAudited(
				otaPermission,
				otaAuditAction,
			)
			otaFamilyScopedFor := func(
				action familydomain.Action,
			) func(http.HandlerFunc) http.HandlerFunc {
				return func(next http.HandlerFunc) http.HandlerFunc {
					return authHandler.requireAuthentication(
						parentScopedFor(
							options.FamilyService,
							action,
							func(
								response http.ResponseWriter,
								request *http.Request,
								_ string,
							) {
								next(response, request)
							},
						),
					)
				}
			}
			requireOTADeviceRead = otaFamilyScopedFor(
				familydomain.ActionDeviceRead,
			)
			requireOTADeviceManage = otaFamilyScopedFor(
				familydomain.ActionDeviceManage,
			)
		}
		ota.registerRoutes(mux, requireAdmin)
		ota.registerGuardianRoutes(
			mux,
			requireOTADeviceRead,
			requireOTADeviceManage,
		)
	}

	registerBillingRoutes(mux, options, logger)

	return observability.WithRequestLabels(observability.WithRequestMetadata(
		observability.WithAccessLog(
			withGlobalRequestBodyLimit(mux),
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

const maxStandardRequestBodyBytes = 2 << 20

// withGlobalRequestBodyLimit bounds every route before a handler decodes or
// streams its request. Large release uploads set their own, larger limit and
// retain their route-level ReadTimeout override.
func withGlobalRequestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if shouldUseLargeRequestBodyLimit(request.URL.Path) {
			next.ServeHTTP(response, request)
			return
		}
		if request.Body != nil {
			request.Body = http.MaxBytesReader(
				response,
				request.Body,
				maxStandardRequestBodyBytes,
			)
		}
		next.ServeHTTP(response, request)
	})
}

func shouldUseLargeRequestBodyLimit(path string) bool {
	return strings.EqualFold(path, "/api/v1/release-publication/files") ||
		strings.EqualFold(path, "/internal/v1/release-files")
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

func healthHandler(buildVersion string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		if strings.TrimSpace(buildVersion) == "" {
			buildVersion = buildinfo.Current()
		}
		writeJSON(response, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "device-platform",
			"version": buildVersion,
			"time":    time.Now().UTC(),
		})
	}
}

func readyHandler(probes []ReadinessProbe) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		for _, probe := range probes {
			if probe == nil {
				continue
			}
			if err := probe(request.Context()); err != nil {
				writeJSON(response, http.StatusServiceUnavailable, map[string]any{
					"status": "unavailable",
				})
				return
			}
		}

		writeJSON(response, http.StatusOK, map[string]any{
			"status": "ready",
		})
	}
}

func runtimeHandler(response http.ResponseWriter, request *http.Request) {
	httpapi.WriteSuccess(response, request, http.StatusOK, map[string]any{
		"service":          "device-platform",
		"status":           "running",
		"protocol_version": httpapi.SchemaVersion,
	})
}

// publicRegistrationPolicyHandler exposes only the values an unauthenticated
// registration or child-consent form needs. It never returns AI defaults,
// retention settings, moderation policy, credentials, or operator metadata.
func publicRegistrationPolicyHandler(
	reader interface {
		RuntimePolicy(ctx context.Context) (*operationsdomain.RuntimePolicy, error)
	},
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if reader == nil {
			writeError(
				response,
				request,
				http.StatusServiceUnavailable,
				"registration_policy_unavailable",
				"注册信息暂时不可用，请稍后重试",
			)
			return
		}
		policy, err := reader.RuntimePolicy(request.Context())
		if err != nil || policy == nil {
			writeError(
				response,
				request,
				http.StatusServiceUnavailable,
				"registration_policy_unavailable",
				"注册信息暂时不可用，请稍后重试",
			)
			return
		}
		consentVersion := strings.TrimSpace(policy.GuardianConsentVersion)
		if consentVersion == "" {
			writeError(
				response,
				request,
				http.StatusServiceUnavailable,
				"registration_policy_unavailable",
				"注册信息暂时不可用，请稍后重试",
			)
			return
		}
		writeSuccess(response, request, http.StatusOK, map[string]any{
			"registration_enabled":        policy.RegistrationEnabled,
			"phone_verification_required": policy.PhoneVerificationRequired,
			"email_login_enabled":         policy.EmailLoginEnabled,
			"guardian_consent_version":    consentVersion,
		})
	}
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

// featureCenterAuditAction maps a feature-center write route to a stable audit
// action so the audit console distinguishes a config change from a manual
// health check instead of collapsing both into one label.
func featureCenterAuditAction(request *http.Request) string {
	path := request.URL.Path
	switch {
	case strings.HasSuffix(path, "/health-check"):
		return "admin.feature_center.health.checked"
	case strings.HasSuffix(path, "/config"):
		return "admin.feature_center.config.updated"
	default:
		return "admin.feature_center.updated"
	}
}

// featureCenterPermission splits the feature-center route group: reading the
// registry and running a manual health check need the read capability, while
// configuration mutations need the manage capability.
func featureCenterPermission(request *http.Request) string {
	if strings.HasSuffix(request.URL.Path, "/health-check") {
		return authdomain.AdminPermissionFeatureCenterRead
	}
	if isAdminMutation(request.Method) {
		return authdomain.AdminPermissionFeatureCenterWrite
	}
	return authdomain.AdminPermissionFeatureCenterRead
}

// otaPermission splits the OTA route group: reads need the read capability,
// lifecycle and deployment mutations need the manage capability.
func otaPermission(request *http.Request) string {
	if isAdminMutation(request.Method) {
		return authdomain.AdminPermissionOTAManage
	}
	return authdomain.AdminPermissionOTARead
}

// otaAuditAction maps an OTA write route to a stable audit action covering the
// release lifecycle (create, update, publish, pause, withdraw, rollback) and
// deployment retries.
func otaAuditAction(request *http.Request) string {
	path := request.URL.Path
	switch {
	case strings.HasSuffix(path, "/publish"):
		return "admin.ota.release.published"
	case strings.HasSuffix(path, "/pause"):
		return "admin.ota.release.paused"
	case strings.HasSuffix(path, "/withdraw"):
		return "admin.ota.release.withdrawn"
	case strings.HasSuffix(path, "/rollback"):
		return "admin.ota.release.rolled_back"
	case strings.HasSuffix(path, "/retry"):
		return "admin.ota.deployment.retried"
	case strings.HasSuffix(path, "/deployments") &&
		request.Method == http.MethodPost:
		return "admin.ota.deployment.created"
	case strings.Contains(path, "/ota/releases"):
		if request.Method == http.MethodPost {
			return "admin.ota.release.created"
		}
		return "admin.ota.release.updated"
	default:
		return "admin.ota.updated"
	}
}

// statusRecorder captures the response status so an audit wrapper can record
// only successful administrator mutations. It forwards Flush so streaming
// handlers such as release-file downloads keep working behind the wrapper.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (recorder *statusRecorder) WriteHeader(status int) {
	if recorder.written {
		return
	}
	recorder.status = status
	recorder.written = true
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(payload []byte) (int, error) {
	if !recorder.written {
		recorder.status = http.StatusOK
		recorder.written = true
	}
	return recorder.ResponseWriter.Write(payload)
}

func (recorder *statusRecorder) Flush() {
	if flusher, ok := recorder.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
