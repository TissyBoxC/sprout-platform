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
	alarmreminderservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/alarm_reminder/service"
	auditdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/domain"
	audithandler "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/audit/handler"
	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	authservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/service"
	bluetoothaudioservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/bluetooth_audio/service"
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
	screenservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/screen/service"
	serviceversionservice "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/service"
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
	UITextService               *ui_textservice.Service
	EnglishService              *englishservice.Service
	PlaybackService             *playbackservice.Service
	LocalCommandService         *localcommandservice.Service
	RecommendationService       *recommendationservice.Service
	UsageReportService          *usagereportservice.Service
	VoiceTokenIssuer            bindingservice.VoiceTokenIssuer
	VoiceWebSocketURL           string
	VoiceConsentReader          voiceConsentReader
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
	VideoSessionService         *videosessionservice.Service
	AlarmReminderService        *alarmreminderservice.Service
	BluetoothAudioService       *bluetoothaudioservice.Service
	WearableService             *wearableservice.Service
	DeviceMeshService           *devicemeshservice.Service
	ScreenService               *screenservice.Service
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
		authHandler := authHandler{
			service: options.AuthService,
			audit:   options.AuditRecorder,
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
		if options.AlarmReminderService != nil {
			alarmReminderHandler := alarmReminderHandler{service: options.AlarmReminderService}
			mux.HandleFunc(
				"GET /api/v1/alarm-reminders",
				authHandler.requireAuthentication(alarmReminderHandler.listAlarms),
			)
			mux.HandleFunc(
				"POST /api/v1/alarm-reminders",
				authHandler.requireAuthentication(alarmReminderHandler.createAlarm),
			)
			mux.HandleFunc(
				"PUT /api/v1/alarm-reminders/{alarm_id}",
				authHandler.requireAuthentication(alarmReminderHandler.updateAlarm),
			)
			mux.HandleFunc(
				"DELETE /api/v1/alarm-reminders/{alarm_id}",
				authHandler.requireAuthentication(alarmReminderHandler.deleteAlarm),
			)
			mux.HandleFunc(
				"GET /api/v1/alarm-reminders/events",
				authHandler.requireAuthentication(alarmReminderHandler.listEvents),
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
					deviceScreenHandler.reportStatus,
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
					deviceVideoSessionHandler.devicePolicy,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/video-session/status",
					deviceVideoSessionHandler.reportStatus,
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
					deviceAlarmReminderHandler.schedule,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/alarm-reminders/{alarm_id}/events",
					deviceAlarmReminderHandler.reportEvent,
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
					deviceBluetoothAudioHandler.devicePolicy,
				)
				mux.HandleFunc(
					"POST /api/v1/devices/{device_id}/bluetooth-audio/status",
					deviceBluetoothAudioHandler.reportStatus,
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
				voiceConsent:      options.VoiceConsentReader,
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
				authHandler.requireAuthentication(adminHandler.updateParentAIModels),
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
					"PUT /api/v1/admin/releases/{version}/publish",
					authHandler.requireAdminPermissionAction(
						authdomain.AdminPermissionStorageManage,
						"admin.release.published",
						adminHandler.publishRelease,
					),
				)
				mux.HandleFunc(
					"DELETE /api/v1/admin/releases/{version}",
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
					authHandler.requireAuthentication(screenHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/screen/policy",
					authHandler.requireAuthentication(screenHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/screen/devices",
					authHandler.requireAuthentication(screenHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/screen/point-read",
					authHandler.requireAuthentication(screenHandler.dictionary),
				)
				mux.HandleFunc(
					"PUT /api/v1/screen/point-read",
					authHandler.requireAuthentication(screenHandler.putDictionaryEntry),
				)
				mux.HandleFunc(
					"DELETE /api/v1/screen/point-read/{entry_id}",
					authHandler.requireAuthentication(screenHandler.deleteDictionaryEntry),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/screen/capability",
					authHandler.requireAuthentication(screenHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/screen/playback",
					authHandler.requireAuthentication(screenHandler.playback),
				)
				mux.HandleFunc(
					"PUT /api/v1/devices/{device_id}/screen/playback",
					authHandler.requireAuthentication(screenHandler.resolvePlayback),
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
					authHandler.requireAuthentication(videoSessionHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/video-session/policy",
					authHandler.requireAuthentication(videoSessionHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/video-session/devices",
					authHandler.requireAuthentication(videoSessionHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/video-session/capability",
					authHandler.requireAuthentication(videoSessionHandler.capability),
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
					authHandler.requireAuthentication(bluetoothAudioHandler.current),
				)
				mux.HandleFunc(
					"PUT /api/v1/bluetooth-audio/policy",
					authHandler.requireAuthentication(bluetoothAudioHandler.publish),
				)
				mux.HandleFunc(
					"GET /api/v1/bluetooth-audio/devices",
					authHandler.requireAuthentication(bluetoothAudioHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/bluetooth-audio/capability",
					authHandler.requireAuthentication(bluetoothAudioHandler.capability),
				)
				mux.HandleFunc(
					"GET /api/v1/admin/bluetooth-audio/stats",
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						bluetoothAudioHandler.adminStats,
					),
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
					authHandler.requireAuthentication(formFactorHandler.statuses),
				)
				mux.HandleFunc(
					"GET /api/v1/devices/{device_id}/form-factor/capability",
					authHandler.requireAuthentication(formFactorHandler.capability),
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
					authHandler.requireAdminPermission(
						authdomain.AdminPermissionStatsRead,
						meshHandler.adminStats,
					),
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
				deviceHandler.published,
			)
			mux.HandleFunc(
				"GET /api/v1/devices/{device_id}/ui-text/packages/{package_id}/{language}/{platform}",
				deviceHandler.publishedPackage,
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
			authHandler := authHandler{
				service: options.AuthService,
				audit:   options.AuditRecorder,
			}
			requireAuthentication = authHandler.requireAuthentication
			requireAdmin = authHandler.requireAdminPermissionAudited(
				otaPermission,
				otaAuditAction,
			)
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

// featureCenterPermission splits the feature-center route group: reads need
// the read capability, mutations need the manage capability.
func featureCenterPermission(request *http.Request) string {
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
