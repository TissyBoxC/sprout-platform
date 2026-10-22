import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/network/api_client.dart';
import '../domain/timeline.dart';
import '../presentation/timeline_copy.dart';

/// Guardian-facing timeline operations.
///
/// It is read-only on purpose: the timeline is assembled from records other
/// features already store, so there is nothing here to create, edit, or delete.
/// Both calls require a child, which is what the platform enforces with a
/// refusal of its own.
abstract interface class TimelineApi {
  /// One page of [childId]'s timeline. [before] is the previous page's cursor;
  /// an empty value asks for the first page, and the reply's empty cursor means
  /// there is nothing newer to load.
  Future<TimelinePage> fetchEvents({
    required String childId,
    String before = '',
    int limit = 30,
  });

  /// The counters shown above the timeline for [childId].
  Future<TimelineMilestones> fetchMilestones({required String childId});
}

/// Production implementation backed by the authenticated API client.
class HttpTimelineApi implements TimelineApi {
  const HttpTimelineApi(this._apiClient, this._familyId);

  final ApiClient _apiClient;
  final String _familyId;

  String get _base => '/api/v1/families/${Uri.encodeComponent(_familyId)}';

  @override
  Future<TimelinePage> fetchEvents({
    required String childId,
    String before = '',
    int limit = 30,
  }) async {
    final response = await _guarded(
      () => _apiClient.get(
        '$_base/timeline',
        queryParameters: <String, String>{
          // The platform answers 422 without a child, so an empty value is
          // never sent as a request that is known to fail.
          'child_id': childId,
          if (before.isNotEmpty) 'before': before,
          'limit': '$limit',
        },
      ),
    );
    return TimelinePage.fromJson(_dataOf(response));
  }

  @override
  Future<TimelineMilestones> fetchMilestones({required String childId}) async {
    final response = await _guarded(
      () => _apiClient.get(
        '$_base/timeline-milestones',
        queryParameters: <String, String>{'child_id': childId},
      ),
    );
    return TimelineMilestones.fromJson(_dataOf(response));
  }

  /// Runs one call and replaces a coded refusal with a guardian-facing one.
  Future<T> _guarded<T>(Future<T> Function() call) async {
    try {
      return await call();
    } on AppException catch (error) {
      final refusal = _refusalFrom(error);
      if (refusal == null) {
        rethrow;
      }
      throw TimelineRefused(refusal);
    }
  }

  Map<String, Object?> _dataOf(Map<String, Object?> response) {
    final data = response['data'];
    return data is Map ? Map<String, Object?>.from(data) : <String, Object?>{};
  }
}

TimelineRefusal? _refusalFrom(AppException error) {
  return switch (readApiErrorCode(_bodyOf(error.cause))) {
    'timeline_invalid_request' => TimelineRefusal.childRequired,
    'timeline_child_not_found' => TimelineRefusal.childNotFound,
    'timeline_invalid_cursor' => TimelineRefusal.cursorExpired,
    'timeline_invalid_limit' => TimelineRefusal.invalidLimit,
    'timeline_unavailable' => TimelineRefusal.unavailable,
    // The remaining code describes an internal failure the guardian cannot
    // fix, so it falls through to the transport's sentence.
    _ => null,
  };
}

/// Extracts the error body the transport attached to a mapped failure.
Object? _bodyOf(Object? cause) {
  if (cause == null) {
    return null;
  }
  final dynamic transportError = cause;
  try {
    return transportError.response?.data;
  } on Object {
    return null;
  }
}
