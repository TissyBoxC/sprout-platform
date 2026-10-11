/// Guardian-facing form-factor contract types.
///
/// A form factor is named technically and never binds to a character or an
/// appearance. The platform composes the action set from the reported
/// capabilities, so this client only ever reads composed, bounded profiles.
library;

/// One named capability or action bit.
class FormFactorBit {
  const FormFactorBit({required this.key, required this.name});

  final String key;
  final String name;

  factory FormFactorBit.fromJson(Map<String, Object?> json) {
    return FormFactorBit(
      key: json['key']?.toString() ?? 'unknown',
      name: json['name']?.toString() ?? 'unknown',
    );
  }
}

/// The composed form-factor capability of one device.
class FormFactorCapability {
  const FormFactorCapability({
    required this.supported,
    required this.technicalName,
    required this.capabilityMask,
    required this.actionMask,
    required this.capabilities,
    required this.actions,
    required this.schemaVersion,
  });

  final bool supported;
  final String technicalName;
  final int capabilityMask;
  final int actionMask;
  final List<FormFactorBit> capabilities;
  final List<FormFactorBit> actions;
  final int schemaVersion;

  factory FormFactorCapability.fromJson(Map<String, Object?> json) {
    return FormFactorCapability(
      supported: json['supported'] == true,
      technicalName: json['technical_name']?.toString() ?? '',
      capabilityMask: _asInt(json['capability_mask']),
      actionMask: _asInt(json['action_mask']),
      capabilities: _bits(json['capabilities']),
      actions: _bits(json['actions']),
      schemaVersion: _asInt(json['schema_version']),
    );
  }
}

List<FormFactorBit> _bits(Object? value) {
  if (value is! List) {
    return const <FormFactorBit>[];
  }
  return value
      .whereType<Map>()
      .map((item) => FormFactorBit.fromJson(Map<String, Object?>.from(item)))
      .toList(growable: false);
}

int _asInt(Object? value) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return 0;
}
