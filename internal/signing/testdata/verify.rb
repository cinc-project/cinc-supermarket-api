#!/usr/bin/env ruby
# frozen_string_literal: true

# verify.rb is the "gold standard" SERVER side of the mixlib-authentication
# signed-header protocol. Where gen_vectors.rb exercises the SIGNER, this
# script exercises the VERIFIER — Mixlib::Authentication::SignatureVerification,
# the exact class the real Chef Supermarket uses to authenticate an incoming
# signed request. The Go test feeds it a request our SignHeaders produced and
# asserts mixlib reports it authenticated.
#
# It speaks one JSON document on stdin and one on stdout so the Go side never
# has to parse Ruby. Two operations:
#
#   {"op":"verify", method, path, host, body_b64, public_key_pem,
#    allowed_versions:[...], time_skew, headers:{"X-Ops-...":"..."}}
#     -> {"authenticated":bool, "version":"1.1", "version_allowed":bool,
#         "reason":"..."}
#
#   {"op":"sign", method, path, body_b64, user_id, timestamp,
#    proto_version, private_key_pem}
#     -> {"headers":{"X-Ops-...":"..."}}
#
# The "sign" op exists so the Go test can mint a *valid* version=1.3 request
# (which our Go signer deliberately cannot produce) and exercise the
# allowed_versions gate below with it.
#
# Requires the mixlib-authentication gem (gem install mixlib-authentication).

require "json"
require "base64"
require "openssl"
require "digest/sha1"
require "mixlib/authentication/signedheaderauth"
require "mixlib/authentication/signatureverification"

# FakeRequest mimics just enough of a Rack/Merb request for
# HTTPAuthenticationRequest and SignatureVerification to read it: the signed
# headers arrive as Rack-style HTTP_* keys in #env, and the body is exposed
# through #raw_post. #params is empty so the verifier hashes the raw body
# (the non-multipart path), which matches a plain signed GET/DELETE.
class FakeRequest
  attr_reader :env, :path

  def initialize(method:, path:, host:, body:, headers:)
    @method = method
    @path = path
    @body = body
    @env = { "HTTP_HOST" => host }
    headers.each do |name, value|
      @env["HTTP_" + name.upcase.tr("-", "_")] = value
    end
  end

  # Named #method explicitly; mixlib calls request.method for the verb.
  def method
    @method
  end

  def raw_post
    @body
  end

  def params
    {}
  end
end

def parse_version(headers)
  sign = headers.find { |k, _| k.downcase == "x-ops-sign" }&.last.to_s
  sign[/version=([^;]+)/, 1]
end

def op_verify(req)
  headers = req.fetch("headers")
  version = parse_version(headers)
  allowed = req.fetch("allowed_versions")

  # Gate on allowed_versions BEFORE handing the request to the verifier.
  # This gate is ours, not mixlib's or Supermarket's: it emulates the observed
  # production behaviour (1.3 shares got a 401 from supermarket.chef.io, cause
  # unknown). mixlib's verifier would happily verify a well-formed 1.3
  # signature, so this gate is the only thing that rejects one here.
  unless allowed.include?(version)
    return { "authenticated" => false, "version" => version,
             "version_allowed" => false,
             "reason" => "sign version #{version.inspect} not in supported set #{allowed.inspect}" }
  end

  body = Base64.decode64(req.fetch("body_b64"))
  fake = FakeRequest.new(
    method: req.fetch("method"),
    path: req.fetch("path"),
    host: req.fetch("host"),
    body: body,
    headers: headers
  )
  public_key = OpenSSL::PKey::RSA.new(req.fetch("public_key_pem"))
  skew = req.fetch("time_skew")

  verifier = Mixlib::Authentication::SignatureVerification.new(fake)
  # authenticate_request returns a truthy SignatureResponse when the
  # signature, timestamp, and content hash all check out.
  result = verifier.authenticate_request(public_key, skew)

  {
    "authenticated" => !result.nil?,
    "version" => version,
    "version_allowed" => true,
    "valid_signature" => verifier.valid_signature?,
    "valid_timestamp" => verifier.valid_timestamp?,
    "valid_content_hash" => verifier.valid_content_hash?,
  }
rescue StandardError => e
  { "authenticated" => false, "version" => version, "version_allowed" => true,
    "reason" => "#{e.class}: #{e.message}" }
end

def op_sign(req)
  key = OpenSSL::PKey::RSA.new(req.fetch("private_key_pem"))
  signer = Mixlib::Authentication::SignedHeaderAuth.signing_object(
    http_method: req.fetch("method"),
    path: req.fetch("path"),
    body: Base64.decode64(req.fetch("body_b64")),
    timestamp: req.fetch("timestamp"),
    user_id: req.fetch("user_id"),
    proto_version: req.fetch("proto_version")
  )
  headers = signer.sign(key).transform_keys(&:to_s)
  { "headers" => headers }
end

input = JSON.parse($stdin.read)
out =
  case input.fetch("op")
  when "verify" then op_verify(input)
  when "sign" then op_sign(input)
  else { "error" => "unknown op #{input["op"].inspect}" }
  end
puts JSON.generate(out)
