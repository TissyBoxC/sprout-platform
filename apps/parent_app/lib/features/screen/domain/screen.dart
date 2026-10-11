/// Guardian-facing screen surface contract types.
///
/// The platform stores only bounded scalar policy values, a bounded
/// point-to-read dictionary and a clamped video plan. No pixels, frames or
/// screenshots ever flow through this client.
library;

/// One validated screen policy for the family.
class ScreenPolicy {
  const ScreenPolicy({
    required this.defaultExpression,
    required this.animationEnabled,
    required this.pointReadEnabled,
    required this.videoEnabled,
    required this.videoMaxFps,
    required this.videoMaxBitrateKbps,
    required this.animationFrameMs,
    required this.revision,
  });

  final String defaultExpression;
  final bool animationEnabled;
  final bool pointReadEnabled;
  final bool videoEnabled;
  final int videoMaxFps;
  final int videoMaxBitrateKbps;
  final int animationFrameMs;
  final int revision;

  ScreenPolicy copyWith({
    String? defaultExpression,
    bool? animationEnabled,
    bool? pointReadEnabled,
    bool? videoEnabled,
    int? videoMaxFps,
    int? videoMaxBitrateKbps,
    int? animationFrameMs,
  }) {
    return ScreenPolicy(
      defaultExpression: defaultExpression ?? this.defaultExpression,
      animationEnabled: animationEnabled ?? this.animationEnabled,
      pointReadEnabled: pointReadEnabled ?? this.pointReadEnabled,
      videoEnabled: videoEnabled ?? this.videoEnabled,
      videoMaxFps: videoMaxFps ?? this.videoMaxFps,
      videoMaxBitrateKbps: videoMaxBitrateKbps ?? this.videoMaxBitrateKbps,
      animationFrameMs: animationFrameMs ?? this.animationFrameMs,
      revision: revision,
    );
  }

  Map<String, Object?> toJson() {
    return <String, Object?>{
      'default_expression': defaultExpression,
      'animation_enabled': animationEnabled,
      'point_read_enabled': pointReadEnabled,
      'video_enabled': videoEnabled,
      'video_max_fps': videoMaxFps,
      'video_max_bitrate_kbps': videoMaxBitrateKbps,
      'animation_frame_ms': animationFrameMs,
    };
  }

  factory ScreenPolicy.fromJson(Map<String, Object?> json) {
    return ScreenPolicy(
      defaultExpression: json['default_expression']?.toString() ?? 'happy',
      animationEnabled: json['animation_enabled'] == true,
      pointReadEnabled: json['point_read_enabled'] == true,
      videoEnabled: json['video_enabled'] == true,
      videoMaxFps: _asInt(json['video_max_fps'], 30),
      videoMaxBitrateKbps: _asInt(json['video_max_bitrate_kbps'], 1500),
      animationFrameMs: _asInt(json['animation_frame_ms'], 200),
      revision: _asInt(json['revision'], 0),
    );
  }
}

/// The fixed expression and animation catalogs reported by the platform.
class ScreenCatalog {
  const ScreenCatalog({required this.expressions, required this.animations});

  final List<String> expressions;
  final List<String> animations;

  factory ScreenCatalog.fromJson(Map<String, Object?> json) {
    return ScreenCatalog(
      expressions: _stringList(json['expressions']),
      animations: _stringList(json['animations']),
    );
  }
}

/// One point-to-read dictionary entry.
class PointReadEntry {
  const PointReadEntry({
    required this.id,
    required this.term,
    required this.text,
  });

  final String id;
  final String term;
  final String text;

  Map<String, Object?> toJson() {
    return <String, Object?>{'term': term, 'text': text};
  }

  factory PointReadEntry.fromJson(Map<String, Object?> json) {
    return PointReadEntry(
      id: json['id']?.toString() ?? '',
      term: json['term']?.toString() ?? '',
      text: json['text']?.toString() ?? '',
    );
  }
}

/// One clamped video playback plan for a device.
class VideoPlaybackPlan {
  const VideoPlaybackPlan({required this.bitrateKbps, required this.fps});

  final int bitrateKbps;
  final int fps;

  factory VideoPlaybackPlan.fromJson(Map<String, Object?> json) {
    return VideoPlaybackPlan(
      bitrateKbps: _asInt(json['bitrate_kbps'], 0),
      fps: _asInt(json['fps'], 0),
    );
  }
}

List<String> _stringList(Object? value) {
  if (value is! List) {
    return const <String>[];
  }
  return value
      .map((item) => item.toString())
      .where((item) => item.isNotEmpty)
      .toList(growable: false);
}

int _asInt(Object? value, int fallback) {
  if (value is int) {
    return value;
  }
  if (value is num) {
    return value.toInt();
  }
  return int.tryParse(value?.toString() ?? '') ?? fallback;
}
