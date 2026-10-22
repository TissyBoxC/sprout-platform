/// Life cycle position of the 情绪感知 switch.
///
/// A family without a stored configuration is [disabled]: sensing has to be
/// turned on by a guardian, so an unknown configuration must never read as on.
enum EmotionAvailability { loading, disabled, enabled, unavailable }

/// One coarse emotion from the platform's fixed vocabulary.
///
/// The vocabulary is closed on purpose. Every value describes a feeling a
/// young child can be in, so a distribution never turns into a judgement about
/// the child. [unknown] only exists so a value the app does not recognise can
/// be dropped instead of shown as a raw word.
enum EmotionLabel {
  calm,
  happy,
  curious,
  tired,
  frustrated,
  sad,
  upset,
  unknown,
}

/// How one label moved compared with the previous period of the same length.
///
/// [unknown] is a real answer, not a missing one: the platform reports it when
/// either period has too few samples, and the screen says so instead of
/// drawing a conclusion from noise.
enum EmotionTrend { up, down, flat, unknown }

/// One label's share of a period.
class EmotionLabelShare {
  const EmotionLabelShare({
    required this.label,
    required this.count,
    required this.percentage,
    required this.trend,
  });

  final EmotionLabel label;
  final int count;

  /// The platform's rounded share, 0 to 100. It is read rather than recomputed
  /// so two screens cannot disagree about the same week by one point.
  final int percentage;
  final EmotionTrend trend;

  factory EmotionLabelShare.fromJson(Map<String, Object?> json) {
    return EmotionLabelShare(
      label: emotionLabelFromWire(_string(json['label'])),
      count: _int(json['count']),
      percentage: _int(json['percentage']).clamp(0, 100),
      trend: _trendFrom(_string(json['trend'])),
    );
  }

  static String _string(Object? value) => value is String ? value : '';

  static int _int(Object? value) => value is int ? value : 0;
}

/// The aggregate a guardian is allowed to read.
///
/// There is no field for an instant or a sample: the platform's contract has
/// no way to describe "at 19:42 the child was frustrated", and this type keeps
/// that shape so the screen cannot show one either. [periodStart] and
/// [periodEnd] describe the window, not any moment inside it.
class EmotionSummary {
  const EmotionSummary({
    required this.days,
    required this.periodStart,
    required this.periodEnd,
    required this.totalSamples,
    required this.previousTotalSamples,
    required this.shares,
    required this.averageConfidence,
    required this.isEnabled,
    required this.retentionDays,
  });

  final int days;
  final DateTime? periodStart;
  final DateTime? periodEnd;
  final int totalSamples;

  /// How many samples the comparison period holds. It explains an [unknown]
  /// trend, so the screen can say "数据还太少" truthfully.
  final int previousTotalSamples;

  /// Ordered as the platform lists them: settled and positive first, tiring
  /// next, difficult last. Unrecognised labels are dropped on decode, and a
  /// label with no samples is kept here so the list keeps its shape.
  final List<EmotionLabelShare> shares;
  final int averageConfidence;

  /// Whether sensing was on when this summary was built. It is read from the
  /// summary rather than from the last settings read, so a guardian who turned
  /// sensing off in another tab sees the honest answer here as well.
  final bool isEnabled;
  final int retentionDays;

  bool get hasData => totalSamples > 0;

  /// The labels that were actually recorded, in the platform's order.
  ///
  /// A zero-count label is dropped: a quiet week should read as "these three
  /// came up", not as a wall of zeros.
  List<EmotionLabelShare> get recordedShares {
    return shares.where((share) => share.count > 0).toList(growable: false);
  }

  /// The most frequent label of the period, or [EmotionLabel.unknown] when
  /// nothing was recorded.
  EmotionLabel get dominantLabel {
    final recorded = recordedShares;
    if (recorded.isEmpty) {
      return EmotionLabel.unknown;
    }
    var best = recorded.first;
    for (final share in recorded.skip(1)) {
      if (share.count > best.count) {
        best = share;
      }
    }
    return best.label;
  }

  /// True when at least one label carries a comparable trend, which is what
  /// tells the screen to show the trend line at all.
  bool get hasComparableTrend {
    return recordedShares.any((share) => share.trend != EmotionTrend.unknown);
  }

  factory EmotionSummary.fromJson(Map<String, Object?> json) {
    final total = _int(json['total_samples']);
    final labels = json['labels'];
    final shares = <EmotionLabelShare>[];
    if (labels is List) {
      for (final entry in labels) {
        if (entry is! Map) {
          continue;
        }
        final share = EmotionLabelShare.fromJson(
          Map<String, Object?>.from(entry),
        );
        // An unrecognised label has no name to show and no place in the
        // vocabulary, so it is dropped rather than rendered as a raw word.
        if (share.label == EmotionLabel.unknown) {
          continue;
        }
        shares.add(share);
      }
    }
    return EmotionSummary(
      days: _int(json['days']),
      periodStart: _instant(json['period_start']),
      periodEnd: _instant(json['period_end']),
      totalSamples: total,
      previousTotalSamples: _int(json['previous_total_samples']),
      shares: shares,
      averageConfidence: _int(json['average_confidence']).clamp(0, 100),
      isEnabled: json['is_enabled'] == true,
      retentionDays: _int(json['retention_days']),
    );
  }

  static int _int(Object? value) => value is int ? value : 0;

  static DateTime? _instant(Object? value) =>
      value is String ? DateTime.tryParse(value)?.toUtc() : null;
}

/// One family's 情绪感知 configuration.
class EmotionSettings {
  const EmotionSettings({
    required this.isEnabled,
    required this.retentionDays,
    required this.summaryDefaultDays,
    required this.maxRetentionDays,
  });

  final bool isEnabled;
  final int retentionDays;

  /// The window the platform shows by default. It is read rather than
  /// hard-coded so a product change ships with the platform.
  final int summaryDefaultDays;

  /// The longest retention the platform accepts.
  final int maxRetentionDays;

  /// The retention periods a guardian can pick from.
  ///
  /// The platform accepts 1 to 90, but a hundred chips is not a choice: the
  /// four durations below are the ones a family can reason about, and the
  /// stored value is kept even when it is not one of them.
  List<int> get offeredRetentionDays {
    final ceiling = maxRetentionDays <= 0 ? 90 : maxRetentionDays;
    final offered = <int>[3, 7, 30, 90].where((days) => days <= ceiling);
    final list = offered.toList(growable: true);
    if (list.isEmpty) {
      list.add(ceiling);
    }
    return list;
  }

  factory EmotionSettings.fromJson(Map<String, Object?> json) {
    return EmotionSettings(
      isEnabled: json['is_enabled'] == true,
      retentionDays: _int(json['retention_days']),
      summaryDefaultDays: _int(json['summary_default_days']),
      maxRetentionDays: _int(json['max_retention_days']),
    );
  }

  static int _int(Object? value) => value is int ? value : 0;
}

/// Known refusals, each with its own guardian-facing sentence.
enum EmotionRefusal {
  /// Sensing is off, so there is no aggregate to read.
  disabled,
  invalidRetention,
  invalidDays,
  childNotFound,
  retentionKept,
  unavailable,
}

/// Maps a wire label to the enum. An unknown value becomes
/// [EmotionLabel.unknown] rather than throwing, because one unreadable label
/// must not empty the whole distribution.
EmotionLabel emotionLabelFromWire(String value) {
  return switch (value) {
    'calm' => EmotionLabel.calm,
    'happy' => EmotionLabel.happy,
    'curious' => EmotionLabel.curious,
    'tired' => EmotionLabel.tired,
    'frustrated' => EmotionLabel.frustrated,
    'sad' => EmotionLabel.sad,
    'upset' => EmotionLabel.upset,
    _ => EmotionLabel.unknown,
  };
}

EmotionTrend _trendFrom(String value) {
  return switch (value) {
    'up' => EmotionTrend.up,
    'down' => EmotionTrend.down,
    'flat' => EmotionTrend.flat,
    // A trend the app cannot read is never upgraded to a direction.
    _ => EmotionTrend.unknown,
  };
}

/// The two windows a guardian can choose between.
const List<int> emotionWindowDays = <int>[7, 30];
