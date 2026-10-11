/// Guardian-facing video companionship contract types.
///
/// The platform stores call metadata only; media never flows through this
/// client. A revoked camera or microphone permission ends the call with a
/// first-class reason instead of leaving it dangling.
library;

/// One guardian-managed video contact.
class VideoContact {
  const VideoContact({
    required this.id,
    required this.childId,
    required this.displayName,
    required this.role,
    required this.enabled,
  });

  final String id;
  final String childId;
  final String displayName;
  final String role;
  final bool enabled;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'child_id': childId,
      'display_name': displayName,
      'role': role,
      'enabled': enabled,
    };
  }

  factory VideoContact.fromJson(Map<String, Object?> json) {
    return VideoContact(
      id: json['id']?.toString() ?? '',
      childId: json['child_id']?.toString() ?? '',
      displayName: json['display_name']?.toString() ?? '',
      role: json['role']?.toString() ?? 'guardian',
      enabled: json['enabled'] == true,
    );
  }
}

/// One stored call metadata row.
class VideoCall {
  const VideoCall({
    required this.id,
    required this.deviceId,
    required this.contactId,
    required this.initiator,
    required this.state,
    required this.quality,
    required this.endReason,
  });

  final String id;
  final String deviceId;
  final String contactId;
  final String initiator;
  final String state;
  final String quality;
  final String endReason;

  bool get isActive => state == 'active' || state == 'ringing';

  factory VideoCall.fromJson(Map<String, Object?> json) {
    return VideoCall(
      id: json['id']?.toString() ?? '',
      deviceId: json['device_id']?.toString() ?? '',
      contactId: json['contact_id']?.toString() ?? '',
      initiator: json['initiator']?.toString() ?? 'guardian',
      state: json['state']?.toString() ?? 'idle',
      quality: json['quality']?.toString() ?? 'unknown',
      endReason: json['end_reason']?.toString() ?? '',
    );
  }
}

/// The video capability of one device as last reported.
class VideoCapability {
  const VideoCapability({
    required this.supported,
    required this.callState,
    required this.quality,
    required this.cameraAllowed,
    required this.microphoneAllowed,
  });

  final bool supported;
  final String callState;
  final String quality;
  final bool cameraAllowed;
  final bool microphoneAllowed;

  factory VideoCapability.fromJson(Map<String, Object?> json) {
    return VideoCapability(
      supported: json['supported'] == true,
      callState: json['call_state']?.toString() ?? 'idle',
      quality: json['quality']?.toString() ?? 'unknown',
      cameraAllowed: json['camera_allowed'] == true,
      microphoneAllowed: json['microphone_allowed'] == true,
    );
  }
}
