// Package repository resolves the child profile a device session is bound to.
//
// 设备与儿童的绑定关系当前只有一条：设备绑定记录归属于监护人账号（家庭），
// 家庭通常只有一个儿童档案，语音网关按家庭下发儿童语音配置。设备上报的
// 长期记忆必须落到那个儿童身上，而不是像某些配置类接口那样只按家庭处理，
// 否则同一家庭第二个孩子的记忆会串到第一个孩子。
//
// 规则：只在该家庭有且仅有一个儿童档案时返回它；存在多个时返回
// ErrDeviceChildAmbiguous，让调用方以稳定的错误码拒绝同步，而不是猜测。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDeviceChildAmbiguous is returned when a family has more than one child
// profile and the device binding does not identify which one it serves.
var ErrDeviceChildAmbiguous = errors.New("device child binding is ambiguous")

// DeviceChildResolver maps a device's owning family to its child profile.
type DeviceChildResolver struct {
	pool *pgxpool.Pool
}

// NewDeviceChildResolver creates the resolver.
func NewDeviceChildResolver(pool *pgxpool.Pool) *DeviceChildResolver {
	return &DeviceChildResolver{pool: pool}
}

// ResolveFamilyChild returns the family's only child profile id.
//
// It returns "" without an error when the family has no child profile yet, so a
// freshly provisioned device receives "not bound to a child" rather than a
// server error. The device id is accepted for call-site symmetry and future
// per-device child binding; it is not trusted as an ownership claim here.
func (r *DeviceChildResolver) ResolveFamilyChild(
	ctx context.Context,
	familyID string,
	deviceID string,
) (string, error) {
	if r == nil || r.pool == nil {
		return "", errors.New("device child resolver database is unavailable")
	}
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return "", nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text
		FROM child_profiles
		WHERE family_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT 2
	`, familyID)
	if err != nil {
		return "", fmt.Errorf("resolve device child profile: %w", err)
	}
	defer rows.Close()
	childIDs := make([]string, 0, 2)
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			return "", fmt.Errorf("scan device child profile: %w", err)
		}
		childIDs = append(childIDs, childID)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate device child profiles: %w", err)
	}
	switch len(childIDs) {
	case 0:
		return "", nil
	case 1:
		return childIDs[0], nil
	default:
		// The device id is deliberately not included in the error text: it
		// would end up in an operator log next to a family identifier.
		_ = deviceID
		return "", ErrDeviceChildAmbiguous
	}
}
