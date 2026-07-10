# frozen_string_literal: true
require "net/http"
require "json"
require "uri"

module AIOps
  # Anthropic Messages API 的 agentic 循環：模型自主決定調用哪個只讀工具，
  # 直到證據鏈閉合或預算耗盡。
  class AgentLoop
    ENDPOINT = URI("https://api.anthropic.com/v1/messages")

    def initialize(api_key:, model:, tools:, max_steps: 15, max_tokens: 4000)
      @api_key = api_key
      @model = model
      @tools = tools
      @max_steps = max_steps
      @max_tokens = max_tokens
    end

    # 返回最終文本結論
    def run(system:, user:)
      messages = [{ role: "user", content: user }]
      @max_steps.times do |step|
        resp = request(system, messages)
        content = resp["content"] || []
        messages << { role: "assistant", content: content }

        tool_uses = content.select { |b| b["type"] == "tool_use" }
        return extract_text(content) if tool_uses.empty? # 沒再要工具 = 給出結論

        results = tool_uses.map do |tu|
          { type: "tool_result", tool_use_id: tu["id"],
            content: @tools.call(tu["name"], tu["input"] || {}) }
        end
        messages << { role: "user", content: results }

        if step == @max_steps - 2
          messages << { role: "user",
                        content: "取證步數即將用盡。基於已有證據給出最終結論；" \
                                 "無法定位就明確說明卡在哪、缺什麼觀測手段。" }
        end
      end
      "（診斷未在步數預算內收斂）"
    end

    private

    def extract_text(content)
      content.select { |b| b["type"] == "text" }.map { |b| b["text"] }.join
    end

    def request(system, messages)
      body = { model: @model, max_tokens: @max_tokens, system: system,
               messages: messages, tools: @tools.definitions }
      attempt = 0
      begin
        http = Net::HTTP.new(ENDPOINT.host, ENDPOINT.port)
        http.use_ssl = true
        http.read_timeout = 300
        req = Net::HTTP::Post.new(ENDPOINT)
        req["content-type"] = "application/json"
        req["x-api-key"] = @api_key
        req["anthropic-version"] = "2023-06-01"
        req.body = JSON.generate(body)
        res = http.request(req)
        parsed = JSON.parse(res.body)
        unless res.is_a?(Net::HTTPSuccess)
          raise "API #{res.code}: #{parsed.dig('error', 'message')}"
        end
        parsed
      rescue => e
        attempt += 1
        raise unless attempt < 3 && e.message.match?(/API (429|5\d\d)|timeout/i)
        sleep(2**attempt)
        retry
      end
    end
  end
end
