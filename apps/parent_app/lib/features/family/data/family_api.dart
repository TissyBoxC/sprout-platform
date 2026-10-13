import '../../../core/network/api_client.dart';
import '../domain/family_membership.dart';

/// Guardian-facing family membership contracts.
///
/// Every call is scoped to the authenticated account on the platform, so the
/// client never sends a family id except the one returned by the capability
/// payload when redeeming an invitation.
abstract interface class FamilyApi {
  /// Reads the membership summary for the signed-in family.
  Future<FamilyCapability> fetchCapability();

  /// Lists every member in the signed-in family.
  Future<List<FamilyMember>> fetchMembers();

  /// Changes one member's role. Only the owner may call this.
  Future<FamilyMember> updateMemberRole(String userId, String role);

  /// Removes one member. Only the owner may call this.
  Future<void> removeMember(String userId);

  /// Lists every pending or resolved invitation in the family.
  Future<List<FamilyInvitation>> fetchInvitations();

  /// Invites an email address as a guardian or viewer.
  Future<FamilyInvitation> createInvitation({
    required String email,
    required String role,
  });

  /// Revokes one pending invitation.
  Future<void> revokeInvitation(String invitationId);

  /// Lists the pending invitations addressed to the signed-in account's email,
  /// so an invitee can accept one before it has a family id.
  Future<List<FamilyInvitation>> fetchMyInvitations();

  /// Redeems the invitation that matches the signed-in account's email.
  Future<FamilyMember> acceptInvitation({
    required String familyId,
    required String invitationId,
  });
}

/// Production implementation backed by the authenticated API client.
class HttpFamilyApi implements FamilyApi {
  const HttpFamilyApi(this._apiClient);

  final ApiClient _apiClient;

  @override
  Future<FamilyCapability> fetchCapability() async {
    final response = await _apiClient.get('/api/v1/family/capability');
    return FamilyCapability.fromJson(_requiredMap(response['data'], 'data'));
  }

  @override
  Future<List<FamilyMember>> fetchMembers() async {
    final response = await _apiClient.get('/api/v1/family/members');
    return _memberList(response['data']);
  }

  @override
  Future<FamilyMember> updateMemberRole(String userId, String role) async {
    final response = await _apiClient.put(
      '/api/v1/family/members/${Uri.encodeComponent(userId)}',
      body: <String, Object?>{'role': role},
    );
    return _requiredMember(response['data']);
  }

  @override
  Future<void> removeMember(String userId) async {
    await _apiClient.delete(
      '/api/v1/family/members/${Uri.encodeComponent(userId)}',
    );
  }

  @override
  Future<List<FamilyInvitation>> fetchInvitations() async {
    final response = await _apiClient.get('/api/v1/family/invitations');
    return _invitationList(response['data']);
  }

  @override
  Future<FamilyInvitation> createInvitation({
    required String email,
    required String role,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/family/invitations',
      body: <String, Object?>{'email': email, 'role': role},
    );
    return _requiredInvitation(response['data']);
  }

  @override
  Future<void> revokeInvitation(String invitationId) async {
    await _apiClient.delete(
      '/api/v1/family/invitations/${Uri.encodeComponent(invitationId)}',
    );
  }

  @override
  Future<List<FamilyInvitation>> fetchMyInvitations() async {
    final response = await _apiClient.get('/api/v1/family/invitations/mine');
    return _invitationList(response['data']);
  }

  @override
  Future<FamilyMember> acceptInvitation({
    required String familyId,
    required String invitationId,
  }) async {
    final response = await _apiClient.post(
      '/api/v1/families/${Uri.encodeComponent(familyId)}'
      '/invitations/${Uri.encodeComponent(invitationId)}/accept',
    );
    return _requiredMember(response['data']);
  }
}

Map<String, Object?> _requiredMap(Object? value, String field) {
  if (value is Map) {
    return Map<String, Object?>.from(value);
  }
  throw FormatException('missing $field');
}

List<FamilyMember> _memberList(Object? value) {
  final data = _requiredMap(value, 'family member data');
  final members = data['members'];
  if (members is! List) {
    return const <FamilyMember>[];
  }
  return members
      .whereType<Map>()
      .map((item) => FamilyMember.fromJson(Map<String, Object?>.from(item)))
      .toList(growable: false);
}

List<FamilyInvitation> _invitationList(Object? value) {
  final data = _requiredMap(value, 'family invitation data');
  final invitations = data['invitations'];
  if (invitations is! List) {
    return const <FamilyInvitation>[];
  }
  return invitations
      .whereType<Map>()
      .map((item) => FamilyInvitation.fromJson(Map<String, Object?>.from(item)))
      .toList(growable: false);
}

FamilyMember _requiredMember(Object? value) {
  final data = _requiredMap(value, 'family member');
  final member = data['member'] ?? data;
  if (member is! Map) {
    throw const FormatException('missing family member');
  }
  return FamilyMember.fromJson(Map<String, Object?>.from(member));
}

FamilyInvitation _requiredInvitation(Object? value) {
  final data = _requiredMap(value, 'family invitation');
  final invitation = data['invitation'] ?? data;
  if (invitation is! Map) {
    throw const FormatException('missing family invitation');
  }
  return FamilyInvitation.fromJson(Map<String, Object?>.from(invitation));
}
