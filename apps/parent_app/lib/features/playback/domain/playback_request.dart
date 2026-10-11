/// Lifecycle of one guardian on-demand playback request.
enum PlaybackStatus {
  pending('pending', '等待设备'),
  acknowledged('acknowledged', '设备已接收'),
  completed('completed', '已播放'),
  failed('failed', '播放失败'),
  cancelled('cancelled', '已取消');

  const PlaybackStatus(this.apiValue, this.label);

  final String apiValue;
  final String label;

  static PlaybackStatus? fromApiValue(String value) {
    for (final status in PlaybackStatus.values) {
      if (status.apiValue == value) {
        return status;
      }
    }
    return null;
  }

  /// Whether the request no longer changes.
  bool get isTerminal =>
      this == PlaybackStatus.completed ||
      this == PlaybackStatus.failed ||
      this == PlaybackStatus.cancelled;
}

/// One requested content package queued for a bound device.
class PlaybackRequest {
  const PlaybackRequest({
    required this.id,
    required this.deviceId,
    required this.packageId,
    required this.source,
    required this.status,
    required this.note,
    required this.requestedAt,
    required this.acknowledgedAt,
    required this.completedAt,
    required this.outcome,
  });

  final String id;
  final String deviceId;
  final String packageId;
  final String source;
  final PlaybackStatus status;
  final String note;
  final DateTime requestedAt;
  final DateTime? acknowledgedAt;
  final DateTime? completedAt;
  final String outcome;

  bool get canCancel => status == PlaybackStatus.pending;

  factory PlaybackRequest.fromJson(Map<String, Object?> json) {
    final status = PlaybackStatus.fromApiValue(_string(json['status']));
    if (status == null) {
      throw const FormatException('unknown playback status');
    }
    return PlaybackRequest(
      id: _string(json['id']),
      deviceId: _string(json['device_id']),
      packageId: _string(json['package_id']),
      source: _string(json['source']),
      status: status,
      note: _string(json['note']),
      requestedAt: _dateTime(json['requested_at']),
      acknowledgedAt: _optionalDateTime(json['acknowledged_at']),
      completedAt: _optionalDateTime(json['completed_at']),
      outcome: _string(json['outcome']),
    );
  }
}

String _string(Object? value) => value is String ? value : '';

DateTime _dateTime(Object? value) {
  return _optionalDateTime(value) ?? DateTime.fromMillisecondsSinceEpoch(0);
}

DateTime? _optionalDateTime(Object? value) {
  if (value is String && value.isNotEmpty) {
    return DateTime.tryParse(value)?.toLocal();
  }
  return null;
}
