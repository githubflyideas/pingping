# frozen_string_literal: true
require "net/http"
require "json"
require "uri"
require "yaml"
require_relative "anomaly"

# 診斷 agent 的手和眼睛。全部只讀，全部審計。
module AIOps
  class Tools
    # 異常掃描的默認關鍵指標集（USE 覆蓋）
    KEY_METRICS = %w[
      cpu_util_pct cpu_iowait_pct psi_cpu_some_avg10 psi_io_some_avg10
      psi_memory_some_avg10 mem_util_pct load1
      tcp_retrans_pct tcp_syn_retrans_ps tcp_listen_drops_ps tcp_timeouts_ps
      conntrack_util_pct
    ].freeze

    def initialize(topology_path:, vm_url:, audit_io: $stderr)
      @topo = YAML.safe_load_file(topology_path)
      @vm = vm_url
      @audit = audit_io
    end

    # ---- 給 Anthropic API 的 tools 定義 ----
    def definitions
      [
        { name: "get_topology",
          description: "獲取環境拓撲：主機清單、線路、依賴關係、已知怪癖。診斷任何問題前先看這個。",
          input_schema: { type: "object", properties: {} } },
        { name: "use_triage",
          description: "Brendan Gregg 式 60 秒排查快照：目標主機最近 60s 所有指標按 " \
                       "Utilization/Saturation/Errors 分組的 avg/max/last。定位問題的標準第一步。",
          input_schema: { type: "object",
                          properties: { host: { type: "string" } }, required: ["host"] } },
        { name: "scan_anomalies",
          description: "對主機的關鍵指標集在指定時間範圍內做異常事件化（穩健z-score+突變檢測），" \
                       "返回異常事件列表（何時、何指標、偏離基線多少、持續多久）。用於回答『那個時間發生了什麼』。",
          input_schema: { type: "object",
                          properties: {
                            host: { type: "string" },
                            minutes_back: { type: "integer", description: "從現在往前掃多少分鐘，默認60" }
                          }, required: ["host"] } },
        { name: "query_metrics",
          description: "對歷史庫執行 PromQL range 查詢，返回降採樣後的序列。可用指標見 use_triage 輸出，" \
                       "均帶 host 標籤，聚合上報粒度10s，另有 *_max 系列保留窗口內尖峰。",
          input_schema: { type: "object",
                          properties: {
                            promql: { type: "string" },
                            minutes_back: { type: "integer" },
                            step_s: { type: "integer", description: "採樣步長秒，默認30" }
                          }, required: %w[promql minutes_back] } },
        { name: "diff_snapshot",
          description: "故障點前後對比（PCP pmdiff 語義）。給定懷疑時刻 t，返回：①指標窗口對比" \
                       "（前/後均值、變化比率、翻倍/腰斬以上才報、出現/消失的指標）②狀態對比" \
                       "（進程出現/消失、線程數與R/S/D狀態分佈變化、sysctl監視清單變更、" \
                       "resolv.conf/路由表/監聽端口/防火牆配置文件變更）。回答『什麼變了』的首選工具。" \
                       "僅覆蓋最近1小時（agent內存快照）。",
          input_schema: { type: "object",
                          properties: {
                            host: { type: "string" },
                            at_ts: { type: "integer", description: "懷疑時刻的 unix 時間戳" },
                            before_s: { type: "integer", description: "前窗口秒數，默認120" },
                            after_s: { type: "integer", description: "後窗口秒數，默認120" }
                          }, required: %w[host at_ts] } },
        { name: "flight_recorder",
          description: "從目標主機 agent 內存拉取最近 N 秒的 1 秒原始粒度數據（歷史庫是10s聚合，" \
                       "幾十秒級間歇性問題會被平均掉，用這個看真實形態）。僅限最近1小時，metric_filter 為子串過濾。",
          input_schema: { type: "object",
                          properties: {
                            host: { type: "string" },
                            seconds: { type: "integer" },
                            metric_filter: { type: "string" }
                          }, required: %w[host seconds] } }
      ]
    end

    # ---- 執行 ----
    def call(name, input)
      @audit.puts("[audit] #{Time.now.strftime('%H:%M:%S')} tool=#{name} input=#{input.to_json}")
      result = case name
               when "get_topology"    then @topo
               when "use_triage"      then use_triage(input["host"])
               when "scan_anomalies"  then scan_anomalies(input["host"], input["minutes_back"] || 60)
               when "query_metrics"   then query_metrics(input["promql"], input["minutes_back"], input["step_s"] || 30)
               when "flight_recorder" then flight_recorder(input["host"], input["seconds"], input["metric_filter"])
               when "diff_snapshot"   then diff_snapshot(input["host"], input["at_ts"],
                                                         input["before_s"] || 120, input["after_s"] || 120)
               else { "error" => "unknown tool #{name}" }
               end
      JSON.generate(result)[0, 30_000] # 防止單次工具結果撐爆上下文
    rescue => e
      JSON.generate("error" => e.message)
    end

    private

    def agent_addr(host)
      addr = @topo.dig("agents", host) or raise "拓撲中沒有主機 #{host} 的 agent 地址"
      addr
    end

    def http_get_json(url)
      res = Net::HTTP.get_response(URI(url))
      raise "#{url} -> HTTP #{res.code}" unless res.is_a?(Net::HTTPSuccess)
      JSON.parse(res.body)
    end

    def use_triage(host)
      http_get_json("http://#{agent_addr(host)}/triage")
    end

    def flight_recorder(host, seconds, filter)
      seconds = [[seconds.to_i, 10].max, 3600].min
      data = http_get_json("http://#{agent_addr(host)}/window?sec=#{seconds}")
      if filter && !filter.empty?
        data["samples"].each do |s|
          s["v"] = s["v"].select { |k, _| k.include?(filter) }
        end
      end
      # 二次防護：點數過多時只保留過濾後内容
      data
    end

    def diff_snapshot(host, at_ts, before_s, after_s)
      http_get_json("http://#{agent_addr(host)}/diff?t=#{at_ts.to_i}" \
                    "&before=#{before_s.to_i}&after=#{after_s.to_i}")
    end

    def query_metrics(promql, minutes_back, step)
      now = Time.now.to_i
      params = URI.encode_www_form(
        query: promql, start: now - minutes_back * 60, end: now, step: step
      )
      raw = http_get_json("#{@vm}/api/v1/query_range?#{params}")
      series = Array(raw.dig("data", "result")).first(8).map do |r|
        { "labels" => r["metric"],
          "points" => r["values"].map { |ts, v| [ts.to_i, v.to_f.round(4)] } }
      end
      { "series" => series }
    end

    def scan_anomalies(host, minutes_back)
      events = []
      KEY_METRICS.each do |m|
        raw = query_metrics(%(#{m}_max{host="#{host}"}), minutes_back, 10)
        pts = raw.dig("series", 0, "points") || next
        events.concat(AIOps::Anomaly.scan(m, pts))
      end
      events.sort_by! { |e| e["start_ts"] || e["around_ts"] || 0 }
      { "host" => host, "window_min" => minutes_back,
        "events" => events, "note" => events.empty? ? "關鍵指標集無異常" : nil }.compact
    end
  end
end
