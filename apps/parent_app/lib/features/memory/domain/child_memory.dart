/// Where a long-term memory came from.
///
/// A guardian entry outranks the device: the platform refuses to let a device
/// upload overwrite a fact a parent corrected by hand, so the origin decides
/// whether the guardian can still edit the value.
enum MemorySource {
  conversation,
  guardian,
  device;

  static MemorySource fromWire(String value) {
    return switch (value) {
      'guardian' => MemorySource.guardian,
      'device' => MemorySource.device,
      _ => MemorySource.conversation,
    };
  }

  String get label {
    return switch (this) {
      MemorySource.conversation => '对话中记下',
      MemorySource.guardian => '家长设置',
      MemorySource.device => '设备上报',
    };
  }
}

/// One remembered fact about a child.
///
/// The value can contain a child's personal information, so it is never written
/// to logs or analytics and only ever rendered on this guardian-only screen.
class ChildMemory {
  const ChildMemory({
    required this.id,
    required this.childId,
    required this.memoryKey,
    required this.memoryValue,
    required this.confidence,
    required this.source,
    required this.revision,
    required this.updatedAt,
  });

  final String id;
  final String childId;
  final String memoryKey;
  final String memoryValue;
  final double confidence;
  final MemorySource source;
  final int revision;
  final DateTime? updatedAt;

  /// Whether a guardian may still change this entry. Device and conversation
  /// memories are editable too; the flag exists so the screen can explain why
  /// a value the device reported keeps coming back.
  bool get isGuardianAuthored => source == MemorySource.guardian;

  factory ChildMemory.fromJson(Map<String, dynamic> json) {
    return ChildMemory(
      id: _stringValue(json['id']),
      childId: _stringValue(json['child_id']),
      memoryKey: _stringValue(json['memory_key']),
      memoryValue: _stringValue(json['memory_value']),
      confidence: _doubleValue(json['confidence']),
      source: MemorySource.fromWire(_stringValue(json['source'])),
      revision: _intValue(json['revision']),
      updatedAt: DateTime.tryParse(_stringValue(json['updated_at'])),
    );
  }
}

String _stringValue(Object? value) => value is String ? value : '';

int _intValue(Object? value) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return 0;
}

double _doubleValue(Object? value) {
  if (value is num) {
    return value.toDouble();
  }
  return 0;
}
