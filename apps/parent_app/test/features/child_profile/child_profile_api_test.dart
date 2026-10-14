import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/child_profile/data/child_profile_api.dart';
import 'package:parent_app/features/child_profile/domain/child_profile.dart';

void main() {
  test('parses the children envelope with age, interests and categories',
      () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'children': <Object?>[
            <String, Object?>{
              'child_id': 'child-1',
              'family_id': 'family-1',
              'nickname': '朵朵',
              'age_tier': 'age_5_6',
              'interests': <Object?>['animals', 'music'],
              'content_categories': <Object?>['story', 'english'],
              'guardian_consent': true,
              'created_at': '2026-10-01T00:00:00Z',
              'updated_at': '2026-10-09T00:00:00Z',
            },
          ],
        },
      },
    );
    final api = ChildProfileApi(client);

    final children = await api.list();

    expect(client.lastGetPath, '/api/v1/children');
    expect(children, hasLength(1));
    final child = children.single;
    expect(child.childId, 'child-1');
    expect(child.nickname, '朵朵');
    expect(child.ageTier, ChildAgeTier.age5To6);
    expect(child.interests, <String>['animals', 'music']);
    expect(child.contentCategories, <ChildContentCategory>[
      ChildContentCategory.story,
      ChildContentCategory.english,
    ]);
    expect(child.guardianConsent, isTrue);
  });

  test('returns an empty list when the platform omits children', () async {
    final api = ChildProfileApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{},
        },
      ),
    );

    expect(await api.list(), isEmpty);
  });

  test('creates a child with the draft wire values and consent', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'child': _childJson()},
      },
    );
    final api = ChildProfileApi(client);

    final created = await api.create(
      const ChildProfileDraft(
        nickname: '朵朵',
        ageTier: ChildAgeTier.age5To6,
        interests: ['animals'],
        contentCategories: [ChildContentCategory.story],
        guardianConsent: true,
      ),
    );

    expect(created.nickname, '朵朵');
    expect(client.lastPostPath, '/api/v1/children');
    expect(client.lastPostBody, <String, Object?>{
      'nickname': '朵朵',
      'age_tier': 'age_5_6',
      'interests': <String>['animals'],
      'content_categories': <String>['story'],
      'guardian_consent': true,
    });
  });

  test('updates a child without resending guardian consent', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{'child': _childJson()},
      },
    );
    final api = ChildProfileApi(client);

    await api.update(
      'child-1',
      const ChildProfileDraft(
        nickname: '朵朵',
        ageTier: ChildAgeTier.age7To8,
        interests: ['space'],
        contentCategories: [ChildContentCategory.poetry],
        guardianConsent: false,
      ),
    );

    expect(client.lastPutPath, '/api/v1/children/child-1');
    expect(client.lastPutBody, <String, Object?>{
      'nickname': '朵朵',
      'age_tier': 'age_7_8',
      'interests': <String>['space'],
      'content_categories': <String>['poetry'],
    });
  });

  test('deletes a child on its identifier path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = ChildProfileApi(client);

    await api.delete('child_2');

    expect(client.deletePaths, <String>['/api/v1/children/child_2']);
  });

  test('rejects an unknown age tier instead of guessing', () {
    expect(
      () => ChildProfile.fromJson(
        _childJson()..['age_tier'] = 'age_9_10',
      ),
      throwsFormatException,
    );
  });
}

Map<String, Object?> _childJson() {
  return <String, Object?>{
    'child_id': 'child-1',
    'family_id': 'family-1',
    'nickname': '朵朵',
    'age_tier': 'age_5_6',
    'interests': <Object?>['animals'],
    'content_categories': <Object?>['story'],
    'guardian_consent': true,
    'created_at': '2026-10-01T00:00:00Z',
    'updated_at': '2026-10-09T00:00:00Z',
  };
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
  String? lastGetPath;
  String? lastPostPath;
  Object? lastPostBody;
  String? lastPutPath;
  Object? lastPutBody;
  final List<String> deletePaths = <String>[];

  @override
  Future<Map<String, Object?>> get(
    String path, {
    Map<String, String>? queryParameters,
  }) async {
    lastGetPath = path;
    return response;
  }

  @override
  Future<Map<String, Object?>> post(String path, {Object? body}) async {
    lastPostPath = path;
    lastPostBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> put(String path, {Object? body}) async {
    lastPutPath = path;
    lastPutBody = body;
    return response;
  }

  @override
  Future<Map<String, Object?>> delete(String path) async {
    deletePaths.add(path);
    return response;
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
