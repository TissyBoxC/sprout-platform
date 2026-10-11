import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/playback/data/playback_api.dart';
import 'package:parent_app/features/playback/domain/playback_request.dart';

void main() {
  test('parses the request list and forwards filters', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'requests': <Object?>[
            <String, Object?>{
              'id': 'req-1',
              'device_id': 'device-1',
              'package_id': 'lesson_story',
              'source': 'guardian',
              'status': 'pending',
              'note': '睡前播放',
              'requested_at': '2026-10-11T09:00:00Z',
            },
          ],
        },
      },
    );
    final api = HttpPlaybackApi(client);

    final requests = await api.fetchRequests(
      deviceId: 'device-1',
      status: 'pending',
    );

    expect(requests, hasLength(1));
    expect(requests.single.status, PlaybackStatus.pending);
    expect(requests.single.packageId, 'lesson_story');
    expect(requests.single.canCancel, isTrue);
    expect(client.lastQueryParameters, <String, String>{
      'device_id': 'device-1',
      'status': 'pending',
    });
  });

  test('omits the filters when listing the whole family', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'requests': <Object?>[]},
      },
    );
    final api = HttpPlaybackApi(client);

    final requests = await api.fetchRequests();

    expect(requests, isEmpty);
    expect(client.lastQueryParameters, isEmpty);
  });

  test('posts a request body and parses the created request', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'request': <String, Object?>{
            'id': 'req-1',
            'device_id': 'device-1',
            'package_id': 'lesson_story',
            'source': 'guardian',
            'status': 'pending',
            'note': '',
            'requested_at': '2026-10-11T09:00:00Z',
          },
        },
      },
    );
    final api = HttpPlaybackApi(client);

    final created = await api.createRequest(
      deviceId: 'device-1',
      packageId: 'lesson_story',
    );

    expect(created.id, 'req-1');
    expect(client.lastPostPath, '/api/v1/playback/requests');
    expect(client.lastPostBody, <String, Object?>{
      'device_id': 'device-1',
      'package_id': 'lesson_story',
      'note': '',
    });
  });

  test('cancels a request on its encoded path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'request': <String, Object?>{
            'id': 'req 1',
            'device_id': 'device-1',
            'package_id': 'lesson_story',
            'source': 'guardian',
            'status': 'cancelled',
            'note': '',
            'requested_at': '2026-10-11T09:00:00Z',
          },
        },
      },
    );
    final api = HttpPlaybackApi(client);

    final cancelled = await api.cancelRequest('req 1');

    expect(cancelled.status, PlaybackStatus.cancelled);
    expect(client.lastPostPath, '/api/v1/playback/requests/req%201/cancel');
  });

  test('rejects an unknown status', () {
    expect(
      () => PlaybackRequest.fromJson(<String, Object?>{'status': 'unknown'}),
      throwsA(isA<FormatException>()),
    );
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  Map<String, String>? lastQueryParameters;
  String? lastPostPath;
  Object? lastPostBody;

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastQueryParameters = queryParameters;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    lastPostPath = path;
    lastPostBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> delete(String path) {
    throw UnimplementedError();
  }

  @override
  Future<Map<String, Object?>> postWithBearerToken(
    String path, {
    Object? body,
    required String bearerToken,
  }) {
    throw UnimplementedError();
  }
}
