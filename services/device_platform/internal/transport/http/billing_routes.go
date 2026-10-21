package http

import (
	"log/slog"
	"net/http"

	authdomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/auth/domain"
	familydomain "github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/family/domain"
)

// scopedSpendingLimit adapts a handler that needs the caller's resolved family
// id onto the parent-scoped route shape.
//
// The family id comes from the resolved membership, not from the path, so a
// caller cannot address another family even if the path happens to be editable.
func (handler billingHandler) scopedSpendingLimit(
	next func(http.ResponseWriter, *http.Request, string),
) func(http.ResponseWriter, *http.Request, string) {
	return func(
		response http.ResponseWriter,
		request *http.Request,
		ownerAccountID string,
	) {
		access, ok := authenticatedFamilyAccess(request)
		if !ok {
			writeError(response, request, http.StatusUnauthorized, "unauthenticated", "请重新登录")
			return
		}
		next(response, request, access.FamilyID)
	}
}

// registerBillingRoutes mounts the billing surfaces.
//
// Three groups, each with its own boundary:
//   - Payment callback: intentionally unauthenticated. The provider cannot hold
//     a platform session; its signature is verified inside the service, and a
//     bad signature is refused there.
//   - Guardian routes: require a session and a family role, then scope every
//     read to the family so another family's order is invisible.
//   - Operator routes: require billing.read for reads and billing.manage for
//     anything that moves money or changes the catalogue.
func registerBillingRoutes(
	mux *http.ServeMux,
	options RouterOptions,
	logger *slog.Logger,
) {
	handler := newBillingHandler(options.Billing.Service, options.Billing.Channels, logger)

	// The provider callback is registered first and unconditionally. A payment
	// provider may retry for hours, so it must get a definitive answer rather
	// than a 404 while billing is disabled.
	mux.HandleFunc(
		"POST /api/v1/billing/payments/notify/{channel}",
		handler.receivePaymentNotification,
	)

	if options.Billing.Service == nil || options.AuthService == nil {
		return
	}
	authHandler := authHandler{
		service: options.AuthService,
		audit:   options.AuditRecorder,
	}
	billingFamilyScopedFor := func(
		action familydomain.Action,
		next func(http.ResponseWriter, *http.Request, string),
	) http.HandlerFunc {
		return authHandler.requireAuthentication(
			parentScopedFor(
				options.FamilyService,
				action,
				func(response http.ResponseWriter, request *http.Request, ownerAccountID string) {
					next(response, request, ownerAccountID)
				},
			),
		)
	}

	// Money moves only for the family owner, not for an invited guardian or a
	// viewer: spending consent and refund requests belong to the account that
	// owns the purchase, mirroring the AI-account manage boundary.
	requireBillingRead := func(
		next func(http.ResponseWriter, *http.Request, string),
	) http.HandlerFunc {
		return billingFamilyScopedFor(familydomain.ActionFamilyRead, next)
	}
	requireBillingManage := func(
		next func(http.ResponseWriter, *http.Request, string),
	) http.HandlerFunc {
		return billingFamilyScopedFor(familydomain.ActionAIAccountManage, next)
	}

	mux.HandleFunc(
		"GET /api/v1/billing/plans",
		requireBillingRead(handler.listPlansGuardian),
	)
	mux.HandleFunc(
		"POST /api/v1/billing/orders",
		requireBillingManage(handler.createOrder),
	)
	mux.HandleFunc(
		"GET /api/v1/billing/orders",
		requireBillingRead(handler.listOrders),
	)
	mux.HandleFunc(
		"GET /api/v1/billing/orders/{order_no}",
		requireBillingRead(handler.getOrder),
	)
	mux.HandleFunc(
		"POST /api/v1/billing/orders/{order_no}/cancel",
		requireBillingManage(handler.cancelOrder),
	)
	mux.HandleFunc(
		"POST /api/v1/billing/orders/{order_no}/pay",
		requireBillingManage(handler.payOrder),
	)
	mux.HandleFunc(
		"POST /api/v1/billing/orders/{order_no}/refund",
		requireBillingManage(handler.requestRefund),
	)
	mux.HandleFunc(
		"GET /api/v1/billing/consent",
		requireBillingRead(handler.getConsent),
	)
	mux.HandleFunc(
		"POST /api/v1/billing/consent",
		requireBillingManage(handler.grantConsent),
	)
	mux.HandleFunc(
		"DELETE /api/v1/billing/consent",
		requireBillingManage(handler.revokeConsent),
	)

	// Family spending cap. The cap is guardian-owned money policy, so reading
	// it needs family read and changing it needs the same owner-only action as
	// checkout: an invited guardian cannot silently raise the limit the owner
	// set for their children.
	spendingLimits := spendingLimitHandler{service: options.Billing.Service}
	mux.HandleFunc(
		"GET /api/v1/families/{family_id}/spending-limit",
		requireBillingRead(handler.scopedSpendingLimit(spendingLimits.getLimit)),
	)
	mux.HandleFunc(
		"PUT /api/v1/families/{family_id}/spending-limit",
		requireBillingManage(handler.scopedSpendingLimit(spendingLimits.setLimit)),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/families/{family_id}/spending-limit",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			spendingLimits.adminGetLimit,
		),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/families/{family_id}/spending-limit/rejections",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			spendingLimits.adminListRejections,
		),
	)

	// Operator console. Reads need billing.read; every mutation needs
	// billing.manage and carries its own audit label.
	mux.HandleFunc(
		"GET /api/v1/admin/billing/orders",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			handler.adminListOrders,
		),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/billing/orders/{order_no}",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			handler.adminGetOrder,
		),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/billing/plans",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			handler.adminListPlans,
		),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/billing/channels",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			handler.adminListChannels,
		),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/billing/plans",
		authHandler.requireAdminPermissionAction(
			authdomain.AdminPermissionBillingManage,
			"admin.billing.plan.created",
			handler.adminCreatePlan,
		),
	)
	mux.HandleFunc(
		"PUT /api/v1/admin/billing/plans/{plan_id}",
		authHandler.requireAdminPermissionAction(
			authdomain.AdminPermissionBillingManage,
			"admin.billing.plan.updated",
			handler.adminUpdatePlan,
		),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/billing/refunds/{refund_no}/approve",
		authHandler.requireAdminPermissionAction(
			authdomain.AdminPermissionBillingManage,
			"admin.billing.refund.approve",
			handler.adminApproveRefund,
		),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/billing/refunds/{refund_no}/reject",
		authHandler.requireAdminPermissionAction(
			authdomain.AdminPermissionBillingManage,
			"admin.billing.refund.reject",
			handler.adminRejectRefund,
		),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/billing/reconciliation",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			handler.adminListReconciliation,
		),
	)
	mux.HandleFunc(
		"GET /api/v1/admin/billing/reconciliation/{run_id}",
		authHandler.requireAdminPermission(
			authdomain.AdminPermissionBillingRead,
			handler.adminGetReconciliation,
		),
	)
	mux.HandleFunc(
		"POST /api/v1/admin/billing/reconciliation",
		authHandler.requireAdminPermissionAction(
			authdomain.AdminPermissionBillingManage,
			"admin.billing.reconciliation.started",
			handler.adminStartReconciliation,
		),
	)
}
