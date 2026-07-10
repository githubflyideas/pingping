# frozen_string_literal: true
require "net/http"
require "json"
require "uri"
require "openssl"
require "base64"

module AIOps
  class Feishu
    def initialize(webhook_url:, secret: nil)
      @url = webhook_url
      @secret = secret
    end

    def enabled? = !(@url.nil? || @url.empty?)

    # 以交互卡片形式推送 markdown 内容
    def push_card(title:, markdown:, color: "blue")
      return warn("[feishu] 未配置 webhook_url，跳过推送") unless enabled?

      payload = {
        msg_type: "interactive",
        card: {
          header: {
            title: { tag: "plain_text", content: title },
            template: color
          },
          elements: [{ tag: "markdown", content: markdown[0, 28_000] }]
        }
      }
      sign!(payload)

      uri = URI(@url)
      res = Net::HTTP.post(uri, JSON.generate(payload),
                           "Content-Type" => "application/json")
      body = JSON.parse(res.body) rescue {}
      if body["code"].to_i != 0
        warn("[feishu] 推送失败: #{res.body[0, 300]}")
      else
        puts "[feishu] 已推送: #{title}"
      end
    end

    private

    # 飞书自定义机器人：签名 = Base64(HMACSHA256(key="{timestamp}\n{secret}", msg=""))
    def sign!(payload)
      return unless @secret && !@secret.empty?
      ts = Time.now.to_i.to_s
      key = "#{ts}\n#{@secret}"
      sig = Base64.strict_encode64(
        OpenSSL::HMAC.digest("SHA256", key, "")
      )
      payload[:timestamp] = ts
      payload[:sign] = sig
    end
  end
end
