#!/usr/bin/env ruby
# frozen_string_literal: true
# 用法: ruby diagnose.rb "從國內訪問又卡了，卡了能有幾十秒，機器負載不高"
require "yaml"
require_relative "lib/tools"
require_relative "lib/claude"
require_relative "lib/feishu"

symptom = ARGV.join(" ").strip
abort("用法: ruby diagnose.rb \"故障症狀描述\"") if symptom.empty?

cfg_path = File.expand_path("../config.yml", __dir__)
cfg = File.exist?(cfg_path) ? YAML.safe_load_file(cfg_path) : {}
api_key = ENV["ANTHROPIC_API_KEY"] || cfg.dig("anthropic", "api_key") ||
          abort("缺少 ANTHROPIC_API_KEY")

tools = AIOps::Tools.new(
  topology_path: File.expand_path("../platform/topology.yml", __dir__),
  vm_url: cfg.dig("vm", "url") || "http://127.0.0.1:8428"
)

SYSTEM = <<~SYS
  你是一名遵循 Brendan Gregg 方法論的資深 SRE，正在對一套私有雲做故障根因診斷。

  工作方式（嚴格遵守）：
  1. 先 get_topology 了解環境，再對相關主機 use_triage（60秒排查）建立全局印象。
  2. 用 USE 方法系統性思考：每個資源依次問 Utilization、Saturation、Errors，
     不憑直覺跳步。負載不高 ≠ 沒有飽和，看 PSI；曲線平滑 ≠ 沒有尖峰，看 _max 和飛行記錄器。
  3. 提出假設 → 選擇最能證偽它的取證動作 → 根據結果收窄或推翻。每一步工具調用
     前先簡短說明你在驗證什麼假設。
  4. 量化，不要猜。所有結論必須引用工具返回的具體數字、時間戳、事件。
     時間對齊是根因判斷的核心證據：症狀窗口與異常事件的起止必須對得上。
  5. 區分因果與相關：兩條曲線同時異常時，用時序先後、拓撲依賴方向判斷誰是因。
  6. 用戶描述的時間可能模糊（「剛才」「又」），優先用 scan_anomalies 找準確異常窗口，
     再用 flight_recorder 看該窗口的 1 秒粒度形態（間歇性問題在 10s 聚合裡會被抹平）。
  7. 「什麼變了」優先於「什麼壞了」：確定懷疑時刻後，先用 diff_snapshot 做前後對比——
     大量事故的根因是變更（配置、進程、路由、防火牆），而不是自發劣化。
     線程狀態分佈裡 D（不可中斷）激增指向 IO/鎖等待，procs_blocked 同理。
     注意相關不等於因果：同時異常的兩個指標可能共享上游根因（如斷電同時導致
     丟包與磁盤故障），需用時序先後和依賴方向判斷。
  7. 誠實面對數據不足：定位不到就明確說明排除了什麼、卡在哪、缺什麼觀測手段。
     這比硬編一個根因有價值。

  最終結論格式（markdown）：
  **結論**: 一句話
  **證據鏈**: 按時間順序，每條帶 [指標/工具 @ 時間 → 數值]
  **根因**: 假設 + 置信度（high/medium/low）
  **已排除**: 查過但排除的方向及依據
  **建議**: 修復建議（只建議，不執行）與缺失的觀測手段
SYS

loop_runner = AIOps::AgentLoop.new(
  api_key: api_key,
  model: cfg.dig("anthropic", "model") || "claude-sonnet-4-6",
  tools: tools,
  max_steps: (cfg.dig("anthropic", "max_steps") || 15).to_i
)

verdict = loop_runner.run(
  system: SYSTEM,
  user: "故障症狀：#{symptom}\n報告時間：#{Time.now.strftime('%Y-%m-%d %H:%M:%S %z')}"
)

puts "\n========== 診斷結論 ==========\n#{verdict}"

# 歸檔供 boss-board 展示（同時也是未來案例記憶的原料）
reports_dir = File.expand_path("../reports", __dir__)
Dir.mkdir(reports_dir) unless Dir.exist?(reports_dir)
File.write(File.join(reports_dir, "diagnose_#{Time.now.strftime('%Y%m%d_%H%M%S')}.md"),
           "**症狀**: #{symptom}\n\n#{verdict}")

AIOps::Feishu.new(
  webhook_url: cfg.dig("feishu", "webhook_url"),
  secret: cfg.dig("feishu", "secret")
).push_card(title: "🧠 AI 診斷", markdown: verdict, color: "blue")
