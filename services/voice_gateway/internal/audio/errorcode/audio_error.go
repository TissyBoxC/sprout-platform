// Package errorcode owns the stable audio error vocabulary shared by the
// device firmware and the voice gateway.
//
// The same strings appear in packages/contracts/schemas/audio_error.schema.json,
// so the device can report one of these codes in diagnostics and the parent
// application can resolve user-facing text without parsing provider messages.
package errorcode

import (
	"errors"

	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/codec"
	"github.com/TissyBoxC/sprout-platform/services/voice_gateway/internal/audio/frame"
)

// CatalogVersion identifies the shared audio error catalog.
const CatalogVersion = "1.0.0"

// Code is one stable audio error identifier.
type Code string

// Firmware-originated codes.
const (
	// CodeCodecNotInitialized means capture or playback ran before the codec
	// was opened, which is retryable once the session starts.
	CodeCodecNotInitialized Code = "audio_codec_not_initialized"

	// CodeCodecInvalidArgument means a caller passed a rejected argument.
	CodeCodecInvalidArgument Code = "audio_codec_invalid_argument"

	// CodeCodecInvalidPacketSize means an encoded packet is outside the fixed
	// profile, so it must be dropped instead of decoded.
	CodeCodecInvalidPacketSize Code = "audio_codec_invalid_packet_size"

	// CodeCodecInvalidPCMSize means a PCM buffer does not hold exactly one
	// frame, which would desynchronise the 20 ms cadence.
	CodeCodecInvalidPCMSize Code = "audio_codec_invalid_pcm_size"

	// CodeCodecEncodeFailed means the capture path could not encode audio.
	CodeCodecEncodeFailed Code = "audio_codec_encode_failed"

	// CodeCodecDecodeFailed means the playback path could not decode audio.
	CodeCodecDecodeFailed Code = "audio_codec_decode_failed"

	// CodeCodecOutputTooSmall means the destination buffer is undersized.
	CodeCodecOutputTooSmall Code = "audio_codec_output_too_small"

	// CodeCodecInternal means an unexpected codec failure with no narrower code.
	CodeCodecInternal Code = "audio_codec_internal"

	// CodeVolumeInvalid means a requested volume or guardian limit is outside
	// the 0-100 range.
	CodeVolumeInvalid Code = "audio_volume_invalid"

	// CodeQueueDuplicateItem means a playback item identifier is already queued.
	CodeQueueDuplicateItem Code = "audio_queue_duplicate_item"

	// CodeQueueFull means the bounded queue reached capacity and rejected the
	// item; the caller may retry once playback drains.
	CodeQueueFull Code = "audio_queue_full"

	// CodeQueuePayloadEmpty means a playback item carried no audio payload.
	CodeQueuePayloadEmpty Code = "audio_queue_payload_empty"

	// CodeQueueInterruptDenied means a request tried to preempt an item that is
	// not interruptible, such as a safety announcement.
	CodeQueueInterruptDenied Code = "audio_queue_interrupt_denied"

	// CodeOutputTimeout means the speaker mixer did not drain a submitted frame
	// before the bounded wait elapsed, which the producer may retry once the
	// playback lane has room.
	CodeOutputTimeout Code = "audio_output_timeout"

	// CodeOutputDiscarded means the mixer dropped a submitted frame after a
	// preemption or clear, so the producer must not report it as played.
	CodeOutputDiscarded Code = "audio_output_discarded"
)

// Voice-gateway-originated codes.
const (
	// CodePacketSizeInvalid means an inbound device packet violates the audio
	// frame contract, so the stream continues without that packet.
	CodePacketSizeInvalid Code = "audio_packet_size_invalid"

	// CodePCMWindowInvalid means the gateway was handed a PCM window that does
	// not hold exactly one frame.
	CodePCMWindowInvalid Code = "audio_pcm_window_invalid"

	// CodeCodecUnavailable means the per-stream codec was not initialized.
	CodeCodecUnavailable Code = "audio_codec_unavailable"
)

// Source identifies which side of the link produced one code.
type Source string

// Sources of an audio error code.
const (
	SourceFirmware     Source = "firmware"
	SourceVoiceGateway Source = "voice_gateway"
)

// Detail is the contract view of one audio error code.
type Detail struct {
	Code       Code
	Source     Source
	Retryable  bool
	Diagnostic string
}

// catalog holds the single source of truth for code metadata. The schema
// example is generated from the same ordering so a reviewer can diff the two.
var catalog = []Detail{
	{CodeCodecNotInitialized, SourceFirmware, true, "codec_session_missing"},
	{CodeCodecInvalidArgument, SourceFirmware, false, "codec_argument_rejected"},
	{CodeCodecInvalidPacketSize, SourceFirmware, false, "packet_outside_profile"},
	{CodeCodecInvalidPCMSize, SourceFirmware, false, "pcm_outside_frame"},
	{CodeCodecEncodeFailed, SourceFirmware, true, "capture_encode_error"},
	{CodeCodecDecodeFailed, SourceFirmware, true, "playback_decode_error"},
	{CodeCodecOutputTooSmall, SourceFirmware, false, "codec_buffer_undersized"},
	{CodeCodecInternal, SourceFirmware, true, "codec_internal_state"},
	{CodeVolumeInvalid, SourceFirmware, false, "volume_outside_range"},
	{CodeQueueDuplicateItem, SourceFirmware, false, "queue_item_id_reused"},
	{CodeQueueFull, SourceFirmware, true, "queue_capacity_reached"},
	{CodeQueuePayloadEmpty, SourceFirmware, false, "queue_payload_missing"},
	{CodeQueueInterruptDenied, SourceFirmware, false, "active_item_protected"},
	{CodeOutputDiscarded, SourceFirmware, false, "mixer_frame_discarded"},
	{CodeOutputTimeout, SourceFirmware, true, "mixer_submit_timeout"},
	{CodePacketSizeInvalid, SourceVoiceGateway, false, "gateway_packet_outside_profile"},
	{CodePCMWindowInvalid, SourceVoiceGateway, false, "gateway_pcm_window_invalid"},
	{CodeCodecUnavailable, SourceVoiceGateway, true, "gateway_codec_missing"},
}

// Catalog returns every audio error code in stable contract order.
func Catalog() []Detail {
	details := make([]Detail, len(catalog))
	copy(details, catalog)
	return details
}

// Describe returns the contract metadata for one code.
//
// The second result is false for a code that is not part of the catalog, which
// lets callers reject an unknown device report instead of inventing defaults.
func Describe(code Code) (Detail, bool) {
	for _, detail := range catalog {
		if detail.Code == code {
			return detail, true
		}
	}
	return Detail{}, false
}

// FirmwareCodeFromAudioCodec maps a firmware audio codec result name to the
// shared contract code. It accepts the stable string produced by
// audio_codec_error_name on the device so the gateway can consume diagnostics
// without sharing a C enum.
func FirmwareCodeFromAudioCodec(errorName string) (Code, bool) {
	switch errorName {
	case "audio_codec_not_initialized":
		return CodeCodecNotInitialized, true
	case "audio_codec_invalid_argument":
		return CodeCodecInvalidArgument, true
	case "audio_codec_invalid_packet_size":
		return CodeCodecInvalidPacketSize, true
	case "audio_codec_invalid_pcm_size":
		return CodeCodecInvalidPCMSize, true
	case "audio_codec_encode_failed":
		return CodeCodecEncodeFailed, true
	case "audio_codec_decode_failed":
		return CodeCodecDecodeFailed, true
	case "audio_codec_output_too_small":
		return CodeCodecOutputTooSmall, true
	case "audio_codec_internal":
		return CodeCodecInternal, true
	default:
		return "", false
	}
}

// FromCodecError maps a gateway codec failure to the shared contract code.
//
// An unknown error maps to CodeCodecUnavailable because an unusable codec is
// the only retryable cause the session can act on; every other codec failure
// already has a narrower, non-retryable mapping.
func FromCodecError(err error) Code {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, codec.ErrInvalidPacketSize):
		return CodePacketSizeInvalid
	case errors.Is(err, codec.ErrInvalidPCMWindows):
		return CodePCMWindowInvalid
	case errors.Is(err, codec.ErrCodecUnavailable):
		return CodeCodecUnavailable
	default:
		return CodeCodecUnavailable
	}
}

// FromFrameError maps an audio frame contract violation to a shared code.
func FromFrameError(err error) Code {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, frame.ErrPayloadTooLarge),
		errors.Is(err, frame.ErrInvalidPayload):
		return CodePacketSizeInvalid
	case errors.Is(err, frame.ErrInvalidProfile):
		return CodePCMWindowInvalid
	default:
		return CodePacketSizeInvalid
	}
}

// IsRetryable reports whether the session may retry after one code.
//
// An unknown code is treated as non-retryable so a malformed device report
// cannot make the gateway loop on a permanent failure.
func IsRetryable(code Code) bool {
	detail, ok := Describe(code)
	return ok && detail.Retryable
}
