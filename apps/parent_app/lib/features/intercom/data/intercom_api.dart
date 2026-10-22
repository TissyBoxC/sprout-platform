import '../../../core/network/api_client.dart';

/// Lifecycle position of one intercom call.
///
/// Only [DeviceCallState.active] carries audio. The parent app must render the
/// ringing and active states differently so a guardian never mistakes a ring
/// for a live microphone.
enum DeviceCallState { ringing, active, ended, missed, declined, cancelled }

/// One call between the guardian and a device.
class DeviceCall {
  const DeviceCall({
    required this.callId,
    required this.deviceId,
    required this.state,
    required this.startedAt,
    required this.deadline,
    this.childId = '',
    this.acceptedAt,
    this.finishedAt,
    this.durationSeconds = 0,
    this.finishReason = '',
  });

  final String callId;
  final String deviceId;
  final String childId;
  final DeviceCallState state;
  final DateTime startedAt;
  final DateTime deadline;
  final DateTime? acceptedAt;
  final DateTime? finishedAt;
  final int durationSeconds;
  final String finishReason;

  bool get isActive => state == DeviceCallState.active;
  bool get isRinging => state == DeviceCallState.ringing;
  bool get isFinished =>
      state == DeviceCallState.ended ||
      state == DeviceCallState.missed ||
      state == DeviceCallState.declined ||
      state == DeviceCallState.cancelled;

  factory DeviceCall.fromJson(Map<String, Object?> json) {
    return DeviceCall(
      callId: _string(json['call_id']),
      deviceId: _string(json['device_id']),
      childId: _string(json['child_id']),
      state: _stateFrom('${json['state'] ?? ''}'),
      startedAt: DateTime.tryParse(_string(json['started_at'])) ?? DateTime.now(),
      deadline: DateTime.tryParse(_string(json['deadline'])) ?? DateTime.now(),
      acceptedAt: DateTime.tryParse(_string(json['accepted_at'])),
      finishedAt: DateTime.tryParse(_string(json['finished_at'])),
      durationSeconds: json['duration_seconds'] is int
          ? json['duration_seconds']! as int
          : 0,
      finishReason: _string(json['finish_reason']),
    );
  }

  static String _string(Object? value) => value is String ? value : '';

  static DeviceCallState _stateFrom(String value) {
    return switch (value) {
      'ringing' => DeviceCallState.ringing,
      'active' => DeviceCallState.active,
      'ended' => DeviceCallState.ended,
      'missed' => DeviceCallState.missed,
      'declined' => DeviceCallState.declined,
      // An unknown state is treated as finished: showing a stale "live" call
      // would be worse than showing the call as over.
      _ => DeviceCallState.cancelled,
    };
  }
}

/// Guardian-facing intercom operations.
abstract interface class IntercomApi {
  Future<DeviceCall> start(String deviceId);

  Future<DeviceCall> fetch(String callId);

  Future<DeviceCall> cancel(String callId);

  Future<DeviceCall> hangUp(String callId);

  Future<List<DeviceCall>> history({int limit});
}

/// Production implementation backed by the authenticated API client.
class HttpIntercomApi implements IntercomApi {
  const HttpIntercomApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String get _base => '/api/v1/families/${Uri.encodeComponent(_familyId)}';

  @override
  Future<DeviceCall> start(String deviceId) async {
    final response = await _apiClient.post(
      '$_base/devices/${Uri.encodeComponent(deviceId)}/intercom-calls',
    );
    return _callFrom(response);
  }

  @override
  Future<DeviceCall> fetch(String callId) async {
    final response = await _apiClient.get(
      '$_base/intercom-calls/${Uri.encodeComponent(callId)}',
    );
    return _callFrom(response);
  }

  @override
  Future<DeviceCall> cancel(String callId) async {
    final response = await _apiClient.post(
      '$_base/intercom-calls/${Uri.encodeComponent(callId)}/cancel',
    );
    return _callFrom(response);
  }

  @override
  Future<DeviceCall> hangUp(String callId) async {
    final response = await _apiClient.post(
      '$_base/intercom-calls/${Uri.encodeComponent(callId)}/hangup',
    );
    return _callFrom(response);
  }

  @override
  Future<List<DeviceCall>> history({int limit = 20}) async {
    final response = await _apiClient.get('$_base/intercom-calls?limit=$limit');
    final data = response['data'];
    final calls = data is Map ? data['calls'] : null;
    if (calls is! List) {
      return const <DeviceCall>[];
    }
    return calls
        .whereType<Map>()
        .map((entry) => DeviceCall.fromJson(Map<String, Object?>.from(entry)))
        .toList(growable: false);
  }

  DeviceCall _callFrom(Map<String, Object?> response) {
    final data = response['data'];
    return DeviceCall.fromJson(
      data is Map ? Map<String, Object?>.from(data) : <String, Object?>{},
    );
  }
}
