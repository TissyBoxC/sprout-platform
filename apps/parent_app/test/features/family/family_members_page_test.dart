import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/data/family_api.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';
import 'package:parent_app/features/family/presentation/family_members_page.dart';

void main() {
  testWidgets('shows members, roles and pending invitations', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          familyApiProvider.overrideWithValue(
            _FakeFamilyApi(
              members: [
                _member('m-1', 'user-owner', FamilyRole.owner),
                _member('m-2', 'user-guardian', FamilyRole.guardian),
              ],
              invitations: [
                _invitation(
                  'inv-1',
                  'invitee@example.com',
                  FamilyInvitationStatus.pending,
                ),
              ],
            ),
          ),
        ],
        child: const MaterialApp(home: FamilyMembersPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('家庭成员'), findsWidgets);
    expect(find.text('家庭协作'), findsOneWidget);
    expect(find.text('所有者'), findsOneWidget);
    expect(find.text('守护者'), findsOneWidget);
    expect(find.text('invitee@example.com'), findsOneWidget);
    expect(find.text('待接受'), findsWidgets);
  });

  testWidgets('invites a family member through the sheet', (tester) async {
    final api = _FakeFamilyApi(
      members: [_member('m-1', 'user-owner', FamilyRole.owner)],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [familyApiProvider.overrideWithValue(api)],
        child: const MaterialApp(home: FamilyMembersPage()),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('邀请家人'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextFormField), 'new@example.com');
    await tester.tap(find.widgetWithText(FilledButton, '发送邀请'));
    await tester.pumpAndSettle();

    expect(api.lastInviteEmail, 'new@example.com');
    expect(api.lastInviteRole, FamilyRole.guardian);
    expect(find.text('new@example.com'), findsOneWidget);
  });

  testWidgets('shows a retry state when loading fails', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          familyApiProvider.overrideWithValue(
            _FakeFamilyApi(members: const [], failLoad: true),
          ),
        ],
        child: const MaterialApp(home: FamilyMembersPage()),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('重新加载'), findsOneWidget);
  });
}

FamilyMember _member(String id, String userId, String role) {
  return FamilyMember(
    id: id,
    userId: userId,
    role: role,
    createdAt: DateTime.utc(2026, 10, 12),
    updatedAt: DateTime.utc(2026, 10, 12),
  );
}

FamilyInvitation _invitation(String id, String email, String status) {
  return FamilyInvitation(
    id: id,
    familyId: 'family-1',
    email: email,
    role: FamilyRole.guardian,
    status: status,
    createdAt: DateTime.utc(2026, 10, 12),
    expiresAt: DateTime.utc(2026, 10, 15),
    acceptedAt: null,
  );
}

class _FakeFamilyApi implements FamilyApi {
  _FakeFamilyApi({
    required List<FamilyMember> members,
    List<FamilyInvitation> invitations = const [],
    this.failLoad = false,
  }) : _members = [...members],
       _invitations = [...invitations];

  final List<FamilyMember> _members;
  final List<FamilyInvitation> _invitations;
  final bool failLoad;

  String? lastInviteEmail;
  String? lastInviteRole;
  int _sequence = 0;

  @override
  Future<FamilyCapability> fetchCapability() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return FamilyCapability(
      familyId: 'family-1',
      ownerUserId: 'user-owner',
      memberCount: _members.length,
      pendingInviteCount: _invitations.where((i) => i.isPending).length,
      schemaVersion: '1.0.0',
    );
  }

  @override
  Future<List<FamilyMember>> fetchMembers() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return List<FamilyMember>.unmodifiable(_members);
  }

  @override
  Future<List<FamilyInvitation>> fetchInvitations() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return List<FamilyInvitation>.unmodifiable(_invitations);
  }

  @override
  Future<FamilyInvitation> createInvitation({
    required String email,
    required String role,
  }) async {
    _sequence += 1;
    lastInviteEmail = email;
    lastInviteRole = role;
    final invitation = _invitation(
      'inv-new-$_sequence',
      email,
      FamilyInvitationStatus.pending,
    );
    _invitations.add(invitation);
    return invitation;
  }

  @override
  Future<FamilyMember> updateMemberRole(String userId, String role) async {
    return _member('m-$userId', userId, role);
  }

  @override
  Future<void> removeMember(String userId) async {}

  @override
  Future<void> revokeInvitation(String invitationId) async {}

  @override
  Future<List<FamilyInvitation>> fetchMyInvitations() async {
    if (failLoad) {
      throw Exception('network down');
    }
    return _invitations.where((item) => item.isPending).toList(growable: false);
  }

  @override
  Future<FamilyMember> acceptInvitation({
    required String familyId,
    required String invitationId,
  }) async {
    return _member('m-joined', 'user-joined', FamilyRole.viewer);
  }
}
