# Test fixtures for the nexus webhook ACME conformance suite.
#
# config.json is loaded by the cert-manager test/acme framework as the
# default solver configuration passed to the webhook. The conformance
# suite requires at minimum a `service` and `apikeysecret`; the values
# here are placeholders matching the example in
# https://github.com/cert-manager/cert-manager/blob/master/test/acme/
# but the actual `nexus-credentials` secret must be created in the
# namespace where the conformance test runs before the suite is
# invoked (see `make test-conformance` in the project root).
{
  "service": "example",
  "apikeysecret": {
    "name": "nexus-credentials",
    "key": "api-key"
  }
}
