# Test fixtures for the nexus webhook ACME conformance suite.
#
# config.json is loaded by the cert-manager test/acme framework as the
# default solver configuration passed to the webhook. The conformance
# suite requires at minimum a `service` and one key reference; the values
# here are placeholders matching the example in
# https://github.com/cert-manager/cert-manager/blob/master/test/acme/
# but the actual `nexus-credentials` secret must be created in the
# namespace where the conformance test runs before the suite is
# invoked (see `make test-conformance` in the project root).
#
# Exactly one key reference is set:
#
#   privatekeysecret  an Ed25519 private key, as written by
#                     `nexus-generate-key --keypair`. The webhook signs
#                     challenge requests with it against the Nexus
#                     server's /api/v3. The matching public key goes on
#                     the server, in plaintext, under
#                     `nexus.server.challenge-public-keys.<service>` --
#                     the server keeps no secret for this client.
#
#   apikeysecret      the legacy shared HMAC key, for /api/v2. Both the
#                     client and the server hold this same secret.
#
# The webhook picks the API version from the kind of key it was given, so
# switching from one field to the other is the whole migration.
#
# config.json below uses the private key; config-hmac.json is the legacy
# form, kept so the HMAC path stays covered.
