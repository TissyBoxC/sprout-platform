/// Guardian-facing Bluetooth audio contract types.
///
/// The platform stores only the family policy and bounded adapter status
/// scalars; no audio, pairing keys or peer addresses flow through this client.
library;

/// Preferred-codec wire values shared with the platform.
enum BluetoothCodec {
  auto('auto', '自动'),
  sbc('sbc', 'SBC'),
  aac('aac', 'AAC'),
  ldac('ldac', 'LDAC');

  const BluetoothCodec(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static BluetoothCodec fromApiValue(String value) {
    for (final codec in BluetoothCodec.values) {
      if (codec.apiValue == value) {
        return codec;
      }
    }
    return BluetoothCodec.auto;
  }
}

/// One validated Bluetooth audio policy for the signed-in family.
class BluetoothAudioPolicy {
  const BluetoothAudioPolicy({
    required this.enabled,
    required this.speakerModeAllowed,
    required this.pairingAllowed,
    required this.autoReconnect,
    required this.preferredCodec,
    required this.maxVolumePercent,
    required this.displayName,
    required this.revision,
  });

  const BluetoothAudioPolicy.conservative()
    : enabled = false,
      speakerModeAllowed = false,
      pairingAllowed = false,
      autoReconnect = true,
      preferredCodec = BluetoothCodec.auto,
      maxVolumePercent = 70,
      displayName = '',
      revision = 0;

  final bool enabled;
  final bool speakerModeAllowed;
  final bool pairingAllowed;
  final bool autoReconnect;
  final BluetoothCodec preferredCodec;
  final int maxVolumePercent;
  final String displayName;
  final int revision;

  factory BluetoothAudioPolicy.fromJson(Map<String, Object?> json) {
    return BluetoothAudioPolicy(
      enabled: json['enabled'] == true,
      speakerModeAllowed: json['speaker_mode_allowed'] == true,
      pairingAllowed: json['pairing_allowed'] == true,
      autoReconnect: json['auto_reconnect'] != false,
      preferredCodec: BluetoothCodec.fromApiValue(
        json['preferred_codec']?.toString() ?? 'auto',
      ),
      maxVolumePercent: _int(json['max_volume_percent'], fallback: 70),
      displayName: json['display_name']?.toString() ?? '',
      revision: _int(json['revision'], fallback: 0),
    );
  }
}

/// Editable policy fields accepted when publishing a new revision.
class BluetoothAudioDraft {
  const BluetoothAudioDraft({
    required this.enabled,
    required this.speakerModeAllowed,
    required this.pairingAllowed,
    required this.autoReconnect,
    required this.preferredCodec,
    required this.maxVolumePercent,
    required this.displayName,
  });

  final bool enabled;
  final bool speakerModeAllowed;
  final bool pairingAllowed;
  final bool autoReconnect;
  final BluetoothCodec preferredCodec;
  final int maxVolumePercent;
  final String displayName;

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'enabled': enabled,
      'speaker_mode_allowed': speakerModeAllowed,
      'pairing_allowed': pairingAllowed,
      'auto_reconnect': autoReconnect,
      'preferred_codec': preferredCodec.apiValue,
      'max_volume_percent': maxVolumePercent,
      'display_name': displayName,
    };
  }
}

/// One device's latest bounded Bluetooth adapter status.
class BluetoothAudioDevice {
  const BluetoothAudioDevice({
    required this.deviceId,
    required this.supported,
    required this.speakerMode,
    required this.adapterState,
    required this.pairedDeviceCount,
    required this.connectedDeviceCount,
    required this.activeCodec,
    required this.volumePercent,
    required this.lastDisconnectReason,
    required this.upToDate,
  });

  final String deviceId;
  final bool supported;
  final bool speakerMode;
  final String adapterState;
  final int pairedDeviceCount;
  final int connectedDeviceCount;
  final String activeCodec;
  final int volumePercent;
  final String lastDisconnectReason;
  final bool upToDate;

  bool get connected => adapterState == 'connected';

  factory BluetoothAudioDevice.fromJson(Map<String, Object?> json) {
    return BluetoothAudioDevice(
      deviceId: json['device_id']?.toString() ?? '',
      supported: json['bluetooth_supported'] == true,
      speakerMode: json['speaker_mode'] == true,
      adapterState: json['adapter_state']?.toString() ?? 'off',
      pairedDeviceCount: _int(json['paired_device_count'], fallback: 0),
      connectedDeviceCount: _int(json['connected_device_count'], fallback: 0),
      activeCodec: json['active_codec']?.toString() ?? 'unknown',
      volumePercent: _int(json['volume_percent'], fallback: 0),
      lastDisconnectReason:
          json['last_disconnect_reason']?.toString() ?? 'none',
      upToDate: json['up_to_date'] == true,
    );
  }
}

/// Stable Chinese label for a device adapter state read from the platform.
String adapterStateLabel(String state) {
  return switch (state) {
    'off' => '已关闭',
    'idle' => '空闲',
    'advertising' => '可被搜索',
    'connecting' => '连接中',
    'connected' => '已连接',
    'error' => '异常',
    _ => '未知',
  };
}

/// Stable Chinese label for an active codec read from the platform.
String activeCodecLabel(String codec) {
  return switch (codec) {
    'unknown' => '未建立',
    'sbc' => 'SBC',
    'aac' => 'AAC',
    'ldac' => 'LDAC',
    _ => codec,
  };
}

/// Stable Chinese label for the last disconnect reason.
String disconnectReasonLabel(String reason) {
  return switch (reason) {
    'none' => '无',
    'user' => '主动断开',
    'range' => '超出范围',
    'peer' => '对方断开',
    'policy' => '策略限制',
    'error' => '异常断开',
    _ => '未知',
  };
}

int _int(Object? value, {required int fallback}) {
  if (value is num) {
    return value.toInt();
  }
  return fallback;
}
