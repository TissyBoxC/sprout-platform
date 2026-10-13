import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/core/network/api_client.dart';
import 'package:parent_app/features/family/data/family_api.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';

void main() {
  test('parses the capability summary', () async {
    final api = HttpFamilyApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'family_id': 'family-1',
            'owner_user_id': 'user-owner',
            'member_count': 3,
            'pending_invite_count': 2,
            'schema_version': '1.0.0',
          },
        },
      ),
    );

    final capability = await api.fetchCapability();

    expect(capability.familyId, 'family-1');
    expect(capability.ownerUserId, 'user-owner');
    expect(capability.memberCount, 3);
    expect(capability.pendingInviteCount, 2);
    expect(capability.schemaVersion, '1.0.0');
  });

  test('parses members and preserves roles', () async {
    final api = HttpFamilyApi(
      _FakeApiClient(
        response: <String, Object?>{
          'data': <String, Object?>{
            'members': <Object?>[
              <String, Object?>{
                'id': 'm-1',
                'user_id': 'user-owner',
                'role': 'owner',
                'created_at': '2026-10-12T00:00:00Z',
                'updated_at': '2026-10-12T00:00:00Z',
              },
              <String, Object?>{
                'id': 'm-2',
                'user_id': 'user-viewer',
                'role': 'viewer',
              },
            ],
          },
        },
      ),
    );

    final members = await api.fetchMembers();

    expect(members, hasLength(2));
    expect(members.first.isOwner, isTrue);
    expect(members.last.role, FamilyRole.viewer);
  });

  test('creates an invitation with the exact body', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'invitation': <String, Object?>{
            'id': 'inv-1',
            'email': 'a@example.com',
            'role': 'guardian',
            'status': 'pending',
            'expires_at': '2026-10-15T00:00:00Z',
          },
        },
      },
    );
    final api = HttpFamilyApi(client);

    final invitation = await api.createInvitation(
      email: 'a@example.com',
      role: FamilyRole.guardian,
    );

    expect(client.lastPostPath, '/api/v1/family/invitations');
    expect(client.lastPostBody, <String, Object?>{
      'email': 'a@example.com',
      'role': 'guardian',
    });
    expect(invitation.isPending, isTrue);
    expect(invitation.role, FamilyRole.guardian);
  });

  test('updates a member role on the encoded path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'member': <String, Object?>{
            'id': 'm-2',
            'user_id': 'user 2',
            'role': 'guardian',
          },
        },
      },
    );
    final api = HttpFamilyApi(client);

    final member = await api.updateMemberRole('user 2', FamilyRole.guardian);

    expect(client.lastPutPath, '/api/v1/family/members/user%202');
    expect(client.lastPutBody, <String, Object?>{'role': 'guardian'});
    expect(member.role, FamilyRole.guardian);
  });

  test('removes a member and revokes an invitation', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{'data': <String, Object?>{}},
    );
    final api = HttpFamilyApi(client);

    await api.removeMember('user 2');
    await api.revokeInvitation('inv 1');

    expect(client.deletePaths, <String>[
      '/api/v1/family/members/user%202',
      '/api/v1/family/invitations/inv%201',
    ]);
  });

  test('accepts an invitation on the encoded family path', () async {
    final client = _FakeApiClient(
      response: <String, Object?>{
        'data': <String, Object?>{
          'member': <String, Object?>{
            'id': 'm-3',
            'user_id': 'user-3',
            'role': 'viewer',
          },
        },
      },
    );
    final api = HttpFamilyApi(client);

    final member = await api.acceptInvitation(
      familyId: 'family 1',
      invitationId: 'inv 9',
    );

    expect(
      client.lastPostPath,
      '/api/v1/families/family%201/invitations/inv%209/accept',
    );
    expect(member.role, FamilyRole.viewer);
  });

  test('rejects an unknown role instead of guessing', () {
    expect(
      () => FamilyMember.fromJson(<String, Object?>{
        'id': 'm-1',
        'user_id': 'user-1',
        'role': 'admin',
      }),
      throwsFormatException,
    );
  });
}

class _FakeApiClient implements ApiClient {
  _FakeApiClient({required this.response});

  final Map<String, Object?> response;
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
