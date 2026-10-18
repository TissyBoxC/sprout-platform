import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/api_error_mapper.dart';
import '../../../core/error/app_exception.dart';
import '../../../core/providers.dart';
import '../data/family_api.dart';
import '../domain/family_membership.dart';

/// Complete membership state rendered by the family page.
class FamilyMembershipState {
  const FamilyMembershipState({
    required this.capability,
    required this.members,
    required this.invitations,
  });

  final FamilyCapability capability;
  final List<FamilyMember> members;
  final List<FamilyInvitation> invitations;

  /// Pending invitations ordered newest first for the invitation list.
  List<FamilyInvitation> get pendingInvitations {
    final result =
        invitations.where((invitation) => invitation.isPending).toList()
          ..sort((a, b) {
            final aTime = a.createdAt;
            final bTime = b.createdAt;
            if (aTime == null || bTime == null) {
              return 0;
            }
            return bTime.compareTo(aTime);
          });
    return result;
  }

  /// Owner first, then guardians, then viewers, keeping a stable order.
  List<FamilyMember> get orderedMembers {
    final result = [...members]..sort(_byRoleThenId);
    return result;
  }

  bool get hasPendingInvitations => pendingInvitations.isNotEmpty;
}

int _byRoleThenId(FamilyMember a, FamilyMember b) {
  final rank = _roleRank(a.role).compareTo(_roleRank(b.role));
  if (rank != 0) {
    return rank;
  }
  return a.userId.compareTo(b.userId);
}

int _roleRank(String role) {
  switch (role) {
    case FamilyRole.owner:
      return 0;
    case FamilyRole.guardian:
      return 1;
    default:
      return 2;
  }
}

/// Owns the family membership, role and invitation state for the guardian UI.
class FamilyController extends AsyncNotifier<FamilyMembershipState> {
  late FamilyApi _api;

  @override
  Future<FamilyMembershipState> build() async {
    _api = ref.read(familyApiProvider);
    return _load();
  }

  Future<void> refresh() async {
    state = await AsyncValue.guard(_load);
  }

  /// Invites an email address as a guardian or viewer, then reloads.
  Future<FamilyInvitation> invite({
    required String email,
    required String role,
  }) async {
    final api = _api;
    final invitation = await api.createInvitation(email: email, role: role);
    await refresh();
    return invitation;
  }

  /// Revokes one pending invitation, then reloads.
  Future<void> revokeInvitation(String invitationId) async {
    final api = _api;
    await api.revokeInvitation(invitationId);
    await refresh();
  }

  /// Changes one member's role, then reloads.
  Future<FamilyMember> updateRole({
    required String userId,
    required String role,
  }) async {
    final api = _api;
    final member = await api.updateMemberRole(userId, role);
    await refresh();
    return member;
  }

  /// Removes one member, then reloads.
  Future<void> removeMember(String userId) async {
    final api = _api;
    await api.removeMember(userId);
    await refresh();
  }

  /// Redeems an invitation for the signed-in account, then reloads so the
  /// joined family immediately replaces the previous membership view.
  Future<FamilyMember> acceptInvitation({
    required String familyId,
    required String invitationId,
  }) async {
    final api = _api;
    final member = await api.acceptInvitation(
      familyId: familyId,
      invitationId: invitationId,
    );
    await refresh();
    return member;
  }

  Future<FamilyMembershipState> _load() async {
    final api = _api;
    final capability = await api.fetchCapability();
    final members = await api.fetchMembers();
    final invitations = await api.fetchInvitations();
    return FamilyMembershipState(
      capability: capability,
      members: members,
      invitations: invitations,
    );
  }

  /// Maps a failure to a guardian-facing message.
  static String messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return mapApiError(error).message;
  }
}

final familyApiProvider = Provider<FamilyApi>((ref) {
  return HttpFamilyApi(ref.read(apiClientProvider));
});

final familyControllerProvider =
    AsyncNotifierProvider<FamilyController, FamilyMembershipState>(
      FamilyController.new,
    );

/// Owns the pending invitations addressed to the signed-in account, used by an
/// invitee that has no family membership yet.
class MyInvitationsController extends AsyncNotifier<List<FamilyInvitation>> {
  late FamilyApi _api;

  @override
  Future<List<FamilyInvitation>> build() async {
    _api = ref.read(familyApiProvider);
    return _api.fetchMyInvitations();
  }

  Future<void> refresh() async {
    final api = _api;
    state = await AsyncValue.guard(api.fetchMyInvitations);
  }

  /// Accepts an invitation and refreshes the invitee's pending list.
  Future<FamilyMember> accept(FamilyInvitation invitation) async {
    final api = _api;
    final member = await api.acceptInvitation(
      familyId: invitation.familyId,
      invitationId: invitation.id,
    );
    await refresh();
    return member;
  }
}

final myInvitationsControllerProvider =
    AsyncNotifierProvider<MyInvitationsController, List<FamilyInvitation>>(
      MyInvitationsController.new,
    );
