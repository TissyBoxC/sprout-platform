// Package domain defines device-to-child binding rules.
//
// 一台设备在同一时刻只服务一个儿童。多孩家庭通过切换「当前儿童」在同一台
// 设备上轮流使用，而不是让设备同时持有多个身份——否则语音对话、长期记忆和
// 成长报告都无法判断当时的说话人是谁。
package domain

import (
	"errors"
	"time"
)

// ErrInvalidBinding is returned when a bind request is not well formed.
var ErrInvalidBinding = errors.New("invalid device child binding")

// ErrChildNotInFamily is returned when the requested child does not belong to
// the device's family. It is deliberately a distinct error so the caller can
// answer "not found" without confirming that the child exists elsewhere.
var ErrChildNotInFamily = errors.New("child does not belong to the device family")

// ErrNoActiveChild is returned when the device currently serves no child.
var ErrNoActiveChild = errors.New("device has no active child")

// ErrDeviceMissing is returned when no binding row exists for the device.
var ErrDeviceMissing = errors.New("device not found")

// Binding is one device's currently active child.
type Binding struct {
	DeviceID    string
	FamilyID    string
	ChildID     string
	ActivatedAt time.Time
}

// History is one past or present association between a device and a child.
//
// A switch keeps the previous row instead of deleting it, so a guardian can see
// which child used the device and when.
type History struct {
	ChildID       string
	IsActive      bool
	ActivatedAt   time.Time
	DeactivatedAt *time.Time
}

// Request binds a child to a device.
type Request struct {
	FamilyID string
	DeviceID string
	ChildID  string
}

// Validate checks the identifying fields.
func (request Request) Validate() error {
	if request.FamilyID == "" || request.DeviceID == "" || request.ChildID == "" {
		return ErrInvalidBinding
	}
	return nil
}

// ResolveState describes why a device currently has no active child.
//
// The three cases need different operator and guardian messages: an unbound
// device is a fresh device, an ambiguous one needs a guardian decision, and an
// unassigned one has children available but none selected.
type ResolveState string

const (
	// ResolveAssigned means the device has an active child.
	ResolveAssigned ResolveState = "assigned"
	// ResolveNoChild means the family has no child profile yet.
	ResolveNoChild ResolveState = "no_child"
	// ResolveUnassigned means children exist but none is active on this device.
	ResolveUnassigned ResolveState = "unassigned"
)

// Resolution is the outcome of resolving a device's active child.
type Resolution struct {
	State   ResolveState
	ChildID string
	// ChildCount is how many child profiles the family has, so the caller can
	// tell "one child, simply not selected yet" from "several children".
	ChildCount int
}
