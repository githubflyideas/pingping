# frozen_string_literal: true

# 曲線 → 異常事件。不把原始點餵給模型，餵事件。
# 方法: 穩健 z-score（median/MAD，抗離群基線污染）+ 持續段合併 + 均值突變檢測。
module AIOps
  module Anomaly
    module_function

    Z_THRESHOLD  = 3.5
    MIN_DURATION = 2 # 至少連續 2 個點才算事件，過濾毛刺

    # series: [[ts, value], ...]  返回事件數組
    def scan(metric, series)
      vals = series.map { |_, v| v }
      return [] if vals.size < 8

      med = median(vals)
      mad = median(vals.map { |v| (v - med).abs })
      events = []

      if mad > 1e-9
        flags = series.map { |_, v| ((v - med) / (1.4826 * mad)).abs >= Z_THRESHOLD }
        events.concat(merge_runs(metric, series, flags, med))
      else
        # 基線恆定（如錯誤計數常年為 0）：任何非基線值都是事件
        flags = series.map { |_, v| (v - med).abs > [med.abs * 0.05, 1e-9].max }
        events.concat(merge_runs(metric, series, flags, med))
      end

      # 均值突變（台階型劣化，z-score 對緩慢台階不敏感）
      if (ev = step_change(metric, series))
        events << ev
      end
      events
    end

    def merge_runs(metric, series, flags, baseline)
      events, run = [], nil
      series.each_with_index do |(ts, v), i|
        if flags[i]
          run ||= { start: ts, peak: v, sum: 0.0, n: 0 }
          run[:end] = ts
          run[:peak] = v if v.abs > run[:peak].abs
          run[:sum] += v
          run[:n] += 1
        elsif run
          events << finalize(metric, run, baseline) if run[:n] >= MIN_DURATION
          run = nil
        end
      end
      events << finalize(metric, run, baseline) if run && run[:n] >= MIN_DURATION
      events
    end

    def finalize(metric, run, baseline)
      {
        "metric" => metric, "type" => "excursion",
        "start" => Time.at(run[:start]).strftime("%H:%M:%S"),
        "end" => Time.at(run[:end]).strftime("%H:%M:%S"),
        "start_ts" => run[:start], "end_ts" => run[:end],
        "duration_s" => run[:end] - run[:start] + 1,
        "baseline" => baseline.round(3),
        "peak" => run[:peak].round(3),
        "avg_during" => (run[:sum] / run[:n]).round(3)
      }
    end

    def step_change(metric, series)
      n = series.size
      return nil if n < 20
      a = series[0...n / 2].map { |_, v| v }
      b = series[n / 2..].map { |_, v| v }
      ma, mb = mean(a), mean(b)
      spread = [stddev(a), 1e-9].max
      return nil unless (mb - ma).abs > 3 * spread && (mb - ma).abs > (ma.abs * 0.3 + 1e-9)
      {
        "metric" => metric, "type" => "step_change",
        "around" => Time.at(series[n / 2][0]).strftime("%H:%M:%S"),
        "around_ts" => series[n / 2][0],
        "before_mean" => ma.round(3), "after_mean" => mb.round(3)
      }
    end

    def median(a)
      s = a.sort
      m = s.size / 2
      s.size.odd? ? s[m] : (s[m - 1] + s[m]) / 2.0
    end

    def mean(a) = a.sum / a.size.to_f

    def stddev(a)
      m = mean(a)
      Math.sqrt(a.sum { |v| (v - m)**2 } / a.size)
    end
  end
end
