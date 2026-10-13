import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/family/application/family_controller.dart';
import 'package:parent_app/features/family/data/family_api.dart';
import 'package:parent_app/features/family/domain/family_membership.dart';

void main() {
  test('loads capability, members and invitations on first build', () async {
    final api = _FakeFamilyApi(
      members: [
        _member('m-1', 'user-viewer', FamilyRole.viewer),
        _member('m-2', 'user-owner', FamilyRole.owner),
        _member('m-3', 'user-guardian', FamilyRole.guardian),
      ],
      invitations: [
        _invitation('inv-1', 'a@example.com', FamilyInvitationStatus.pending),
        _invitation('inv-2', 'b@example.com', FamilyInvitationStatus.revoked),
      ],
    );
    final container = ProviderContainer(
      overrides: [familyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final state = await container.read(familyControllerProvider.future);

    expect(state.members, hasLength(3));
    expect(state.orderedMembers.map((m) => m.role), [
      FamilyRole.owner,
      FamilyRole.guardian,
      FamilyRole.viewer,
    ]);
    expect(state.pendingInvitations, hasLength(1));
    expect(state.hasPendingInvitations, isTrue);
    expect(state.capability.memberCount, 3);
  });

  test('invites a guardian then reloads the roster', () async {
    final api = _FakeFamilyApi(members: const []);
    final container = ProviderContainer(
      overrides: [familyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(familyControllerProvider.future);
    final controller = container.read(familyControllerProvider.notifier);
    final invitation = await controller.invite(
      email: 'new@example.com',
      role: FamilyRole.guardian,
    );

    expect(invitation.email, 'new@example.com');
    expect(api.lastInviteEmail, 'new@example.com');
    expect(api.lastInviteRole, FamilyRole.guardian);
    final state = container.read(familyControllerProvider).value;
    expect(state?.pendingInvitations, hasLength(1));
  });

  test('updates a member role then reloads', () async {
    final api = _FakeFamilyApi(
      members: [_member('m-2', 'user-2', FamilyRole.viewer)],
    );
    final container = ProviderContainer(
      overrides: [familyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(familyControllerProvider.future);
    final controller = container.read(familyControllerProvider.notifier);
    final updated = await controller.updateRole(
      userId: 'user-2',
      role: FamilyRole.guardian,
    );

    expect(updated.role, FamilyRole.guardian);
    expect(api.lastRoleUserId, 'user-2');
    expect(api.lastRole, FamilyRole.guardian);
  });

  test('removes a member and revokes an invitation', () async {
    final api = _FakeFamilyApi(
      members: [_member('m-2', 'user-2', FamilyRole.guardian)],
      invitations: [
        _invitation('inv-1', 'a@example.com', FamilyInvitationStatus.pending),
      ],
    );
    final container = ProviderContainer(
      overrides: [familyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(familyControllerProvider.future);
    final controller = container.read(familyControllerProvider.notifier);
    await controller.removeMember('user-2');
    await controller.revokeInvitation('inv-1');

    expect(api.removedUserIds, ['user-2']);
    expect(api.revokedInvitationIds, ['inv-1']);
    final state = container.read(familyControllerProvider).value;
    expect(state?.members, isEmpty);
    expect(state?.pendingInvitations, isEmpty);
  });

  test('accepts an invitation then reloads', () async {
    final api = _FakeFamilyApi(members: const []);
    final container = ProviderContainer(
      overrides: [familyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    await container.read(familyControllerProvider.future);
    final controller = container.read(familyControllerProvider.notifier);
    final member = await controller.acceptInvitation(
      familyId: 'family-1',
      invitationId: 'inv-1',
    );

    expect(member.role, FamilyRole.viewer);
    expect(api.acceptedFamilyId, 'family-1');
    expect(api.acceptedInvitationId, 'inv-1');
  });

  test('propagates a load failure as an error state', () async {
    final api = _FakeFamilyApi(members: const [], failLoad: true);
    final container = ProviderContainer(
      overrides: [familyApiProvider.overrideWithValue(api)],
    );
    addTearDown(container.dispose);

    final subscription = container.listen(familyControllerProvider, (_, _) {});
    addTearDown(subscription.close);
    await Future<void>.delayed(Duration.zero);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(familyControllerProvider).hasError, isTrue);
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
  String? lastRoleUserId;
  String? lastRole;
  final List<String> removedUserIds = [];
  final List<String> revokedInvitationIds = [];
  String? acceptedFamilyId;
  String? acceptedInvitationId;
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
      pendingInviteCount:
          _invitations.where((i) => i.isPending).length,
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
    final invitation = FamilyInvitation(
      id: 'inv-new-$_sequence',
      familyId: 'family-1',
      email: email,
      role: role,
      status: FamilyInvitationStatus.pending,
      createdAt: DateTime.utc(2026, 10, 12),
      expiresAt: DateTime.utc(2026, 10, 15),
      acceptedAt: null,
    );
    _invitations.add(invitation);
    return invitation;
  }

  @override
  Future<FamilyMember> updateMemberRole(String userId, String role) async {
    lastRoleUserId = userId;
    lastRole = role;
    final member = _member('m-$userId', userId, role);
    final index = _members.indexWhere((item) => item.userId == userId);
    if (index >= 0) {
      _members[index] = member;
    } else {
      _members.add(member);
    }
    return member;
  }

  @override
  Future<void> removeMember(String userId) async {
    removedUserIds.add(userId);
    _members.removeWhere((item) => item.userId == userId);
  }

  @override
  Future<void> revokeInvitation(String invitationId) async {
    revokedInvitationIds.add(invitationId);
    _invitations.removeWhere((item) => item.id == invitationId);
  }

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
    acceptedFamilyId = familyId;
    acceptedInvitationId = invitationId;
    final member = _member('m-joined', 'user-joined', FamilyRole.viewer);
    _members.add(member);
    return member;
  }
}
