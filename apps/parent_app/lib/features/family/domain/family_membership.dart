/// One member of the signed-in family.
///
/// The platform keeps the unique owner plus any guardians or viewers that
/// accepted an invitation. The client never invents a family id: every request
/// is scoped to the authenticated account on the server.
class FamilyMember {
  const FamilyMember({
    required this.id,
    required this.userId,
    required this.role,
    required this.createdAt,
    required this.updatedAt,
  });

  final String id;
  final String userId;
  final String role;
  final DateTime? createdAt;
  final DateTime? updatedAt;

  bool get isOwner => role == FamilyRole.owner;

  factory FamilyMember.fromJson(Map<String, Object?> json) {
    return FamilyMember(
      id: _string(json['id']),
      userId: _string(json['user_id']),
      role: _role(json['role']),
      createdAt: _optionalDateTime(json['created_at']),
      updatedAt: _optionalDateTime(json['updated_at']),
    );
  }
}

/// One pending or resolved email invitation into the family.
class FamilyInvitation {
  const FamilyInvitation({
    required this.id,
    required this.familyId,
    required this.email,
    required this.role,
    required this.status,
    required this.createdAt,
    required this.expiresAt,
    required this.acceptedAt,
  });

  final String id;
  final String familyId;
  final String email;
  final String role;
  final String status;
  final DateTime? createdAt;
  final DateTime? expiresAt;
  final DateTime? acceptedAt;

  bool get isPending => status == FamilyInvitationStatus.pending;

  factory FamilyInvitation.fromJson(Map<String, Object?> json) {
    return FamilyInvitation(
      id: _string(json['id']),
      familyId: _string(json['family_id']),
      email: _string(json['email']),
      role: _role(json['role']),
      status: _string(json['status']),
      createdAt: _optionalDateTime(json['created_at']),
      expiresAt: _optionalDateTime(json['expires_at']),
      acceptedAt: _optionalDateTime(json['accepted_at']),
    );
  }
}

/// Membership summary mirrored from the platform capability endpoint.
class FamilyCapability {
  const FamilyCapability({
    required this.familyId,
    required this.ownerUserId,
    required this.memberCount,
    required this.pendingInviteCount,
    required this.schemaVersion,
  });

  final String familyId;
  final String ownerUserId;
  final int memberCount;
  final int pendingInviteCount;
  final String schemaVersion;

  factory FamilyCapability.fromJson(Map<String, Object?> json) {
    return FamilyCapability(
      familyId: _string(json['family_id']),
      ownerUserId: _string(json['owner_user_id']),
      memberCount: _int(json['member_count']),
      pendingInviteCount: _int(json['pending_invite_count']),
      schemaVersion: _string(json['schema_version']),
    );
  }
}

/// The fixed role catalogue shared with the platform.
class FamilyRole {
  const FamilyRole._();

  static const owner = 'owner';
  static const guardian = 'guardian';
  static const viewer = 'viewer';

  /// Roles that may be granted through an invitation. The unique owner is
  /// never handed out this way, matching the platform guard.
  static const invitable = <String>[guardian, viewer];

  static String label(String role) {
    return const <String, String>{
          owner: '所有者',
          guardian: '守护者',
          viewer: '查看者',
        }[role] ??
        '成员';
  }
}

/// The fixed invitation lifecycle values shared with the platform.
class FamilyInvitationStatus {
  const FamilyInvitationStatus._();

  static const pending = 'pending';
  static const accepted = 'accepted';
  static const revoked = 'revoked';

  static String label(String status) {
    return const <String, String>{
          pending: '待接受',
          accepted: '已接受',
          revoked: '已撤销',
        }[status] ??
        '未知';
  }
}

String _string(Object? value) => value is String ? value.trim() : '';

String _role(Object? value) {
  final role = _string(value);
  if (role == FamilyRole.owner ||
      role == FamilyRole.guardian ||
      role == FamilyRole.viewer) {
    return role;
  }
  throw const FormatException('unknown family role');
}

int _int(Object? value) {
  if (value is num) {
    return value.toInt();
  }
  return 0;
}

DateTime? _optionalDateTime(Object? value) {
  if (value is String && value.trim().isNotEmpty) {
    return DateTime.tryParse(value.trim())?.toLocal();
  }
  return null;
}
