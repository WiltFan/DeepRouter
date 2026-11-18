#!/usr/bin/env python3
"""Offline tests for image-generate/scripts/deeprouter_image.py.

No network: OPENER.open is replaced in every test; a guard fails the test if
anything tries to reach the real opener.

Run:  python3 -I test_deeprouter_image.py
(set DEEPROUTER_IMAGE_SCRIPT to test another copy of the script)
"""

import base64
import contextlib
import email.message
import gc
import importlib.util
import io
import json
import os
import re
import socket
import ssl
import stat
import sys
import tempfile
import unittest
import urllib.error
import urllib.request
import urllib.response
import warnings

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_SCRIPT = os.path.join(HERE, "..", "embed", "scripts", "deeprouter_image.py")
SCRIPT = os.environ.get("DEEPROUTER_IMAGE_SCRIPT") or DEFAULT_SCRIPT

FAKE_KEY = "sk-Ab3dEf9hIjKlMn0pQrStUvWxYz1234567890QWERTY"
OTHER_KEY = "sk-proj-ZZtopSecretValue0987654321abcdefXYZ"  # not the env key, still must be redacted
SIGNED_URL = ("https://bucket.s3.amazonaws.com/out/img.png?X-Amz-Algorithm=AWS4-HMAC-SHA256"
              "&X-Amz-Credential=AKIAEXAMPLE%2F20261116&X-Amz-Expires=3600"
              "&X-Amz-Signature=9f8e7d6c5b4a39281706f5e4d3c2b1a0fedcba9876543210&token=abcSECRETtok#frag")
QUERY_KEY_URL = "https://deeprouter.top/v1beta/models/x:generateContent?key=" + OTHER_KEY
BIG_B64 = base64.b64encode(os.urandom(6000)).decode()
PNG = b"\x89PNG\r\n\x1a\n" + b"\x00" * 32
SVG = b'<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"></svg>'

FILE_KEY = "sk-test-fileCfgKEYvalue000000000000000"   # image config file key (fake)
IMG_ENV_KEY = "sk-test-imageEnvKEYvalue00000000000000"  # DEEPROUTER_IMAGE_API_KEY (fake)

FORBIDDEN = [
    FAKE_KEY, FAKE_KEY[3:], OTHER_KEY, OTHER_KEY[8:], FILE_KEY, FILE_KEY[8:], IMG_ENV_KEY, IMG_ENV_KEY[8:], "X-Amz-Signature", "9f8e7d6c5b4a3928",
    "X-Amz-Credential", "abcSECRETtok", "#frag", "Bearer sk", BIG_B64[:40], BIG_B64[1000:1040],
    "hunter2pass",
]

LEAKY_TEXT = (
    "upstream said: invalid key %s; request headers Authorization: Bearer %s; "
    "DEEPROUTER_API_KEY=%s api_key=%s \"api-key\": \"%s\" retry at %s or %s password=hunter2pass "
    "payload %s" % (FAKE_KEY, FAKE_KEY, FAKE_KEY, OTHER_KEY, OTHER_KEY, SIGNED_URL, QUERY_KEY_URL, BIG_B64)
)


def load_module():
    spec = importlib.util.spec_from_file_location("deeprouter_image_under_test", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


M = load_module()


class FakeResp(object):
    def __init__(self, body, status=200):
        self._body = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.status = status

    def read(self, n=-1):
        return self._body if n is None or n < 0 else self._body[:n]

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class UnexpectedRequest(BaseException):
    """Raised by the fake openers for a request with no outcome left. A BaseException,
    so the script's `except Exception` around a download cannot swallow it."""


class FakeOpener(object):
    """Replays a list of outcomes (FakeResp or exception) and records requests."""

    def __init__(self, *outcomes):
        self.outcomes = list(outcomes)
        self.requests = []

    def open(self, req, timeout=None):
        self.requests.append(req)
        if not self.outcomes:
            raise UnexpectedRequest("unexpected extra request")
        o = self.outcomes.pop(0)
        if isinstance(o, BaseException):
            raise o
        return o


class NeverOpener(object):
    def open(self, req, timeout=None):
        raise AssertionError("request must not be sent")


# HTTPError objects built by the tests. Each one is closed in Base.tearDown: from
# Python 3.15 an unclosed HTTPError warns "Implicitly cleaning up <HTTPError code: msg>"
# whenever it is garbage collected, which can be during a later test's capture. The
# msg here is test fixture text, not script output, so the tests must close what they own.
_TEST_ERRORS = []


def own(err):
    _TEST_ERRORS.append(err)
    return err


def http_error(code, body, url="https://deeprouter.top/v1/images/generations"):
    return own(urllib.error.HTTPError(url, code, "err " + LEAKY_TEXT, {}, io.BytesIO(body)))


def http_error_warns_when_unclosed():
    """True on Pythons (3.15+) whose HTTPError emits a ResourceWarning if collected unclosed."""
    e = urllib.error.HTTPError("https://example.com/", 500, "probe", {}, io.BytesIO())
    try:
        return hasattr(getattr(e, "_closer", None), "warn_message")
    finally:
        e.close()


# Every variable the script reads for its address, key, or config location.
# Each test starts from a temporary HOME with no image config and only the
# fake DEEPROUTER_API_KEY, so the real user's files and keys are never read.
# Proxy variables are cleared too, so a proxy set on the machine running the
# tests never changes where a request is routed.
ENV_VARS = ("DEEPROUTER_API_KEY", "DEEPROUTER_IMAGE_BASE_URL", "DEEPROUTER_IMAGE_API_KEY", "HOME", "USERPROFILE",
            "http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY", "all_proxy", "ALL_PROXY",
            "no_proxy", "NO_PROXY", "REQUEST_METHOD")


class Base(unittest.TestCase):
    def setUp(self):
        self._saved_env = {k: os.environ.get(k) for k in ENV_VARS}
        self.home = tempfile.TemporaryDirectory()
        for k in ENV_VARS:
            os.environ.pop(k, None)
        os.environ["HOME"] = os.environ["USERPROFILE"] = self.home.name
        os.environ["DEEPROUTER_API_KEY"] = FAKE_KEY
        self._opener = M.OPENER
        M.OPENER = NeverOpener()
        # Download retry backoff is recorded, never slept.
        self._sleep = M.backoff_sleep
        self.sleeps = []
        M.backoff_sleep = self.sleeps.append
        # No DNS in tests: the resolver answers self.dns_answer and records each lookup.
        # The script itself never resolves a result host (see FakeIPDownloadTests).
        self._getaddrinfo = socket.getaddrinfo
        self.lookups = []
        self.dns_answer = "93.184.216.34"
        socket.getaddrinfo = lambda host, port, *a, **kw: self.lookups.append(host) or [
            (socket.AF_INET, socket.SOCK_STREAM, 6, "", (self.dns_answer, port or 443))]
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        M.OPENER = self._opener
        M.backoff_sleep = self._sleep
        socket.getaddrinfo = self._getaddrinfo
        for k, v in self._saved_env.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
        while _TEST_ERRORS:
            _TEST_ERRORS.pop().close()
        self.tmp.cleanup()
        self.home.cleanup()

    def capture(self, fn, *args, **kw):
        """Run fn with stdout/stderr captured and ResourceWarnings on.

        Garbage is collected before leaving the capture, so a ResourceWarning for
        anything the script left unclosed lands in the captured stderr (and is
        checked by assert_clean) instead of in some later test's output.
        """
        out, err = io.StringIO(), io.StringIO()
        code = None
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err), warnings.catch_warnings():
            warnings.simplefilter("always", ResourceWarning)
            try:
                fn(*args, **kw)
            except SystemExit as e:
                code = e.code
            gc.collect()
        return code, out.getvalue(), err.getvalue()

    def assert_clean(self, text, max_len=700):
        for bad in FORBIDDEN:
            self.assertNotIn(bad, text, "leaked %r in output: %r" % (bad[:30], text[:400]))
        self.assertNotRegex(text, r"[A-Za-z0-9+/]{100,}", "long Base64-like run in output")
        # The length cap is about redaction/truncation, not about where the OS puts
        # temp dirs (macOS TMPDIR is ~45 chars longer than /tmp), so the test's own
        # directory counts as a fixed-size placeholder.
        short = text
        for d in sorted({os.path.realpath(self.tmp.name), os.path.abspath(self.tmp.name)}, key=len, reverse=True):
            short = short.replace(d, "<tmp>")
        self.assertLessEqual(len(short), max_len, "output not truncated: %d chars" % len(short))

    def gen_args(self, *extra):
        return ["generate", "--confirm", "--prompt", "a fox", "--out-dir", self.tmp.name] + list(extra)

    def recovery_files(self, d=None):
        d = d or self.tmp.name
        return sorted(os.path.join(d, n) for n in os.listdir(d) if n.startswith(M.RECOVER_PREFIX))


class RedactTests(Base):
    def test_redact_removes_all_secret_shapes(self):
        r = M.redact(LEAKY_TEXT)
        self.assert_clean(r, max_len=M.MAX_MSG + 3)

    def test_redact_case_insensitive(self):
        os.environ["DEEPROUTER_API_KEY"] = ""
        r = M.redact("AUTHORIZATION: BEARER %s SK-%s Api-Key=%s Access_Token: abcSECRETtok KEY=hunter2pass"
                     % (OTHER_KEY, OTHER_KEY[3:], OTHER_KEY))
        self.assert_clean(r)

    def test_safe_url_strips_query_fragment_userinfo(self):
        self.assertEqual(M.safe_url(SIGNED_URL), "https://bucket.s3.amazonaws.com/out/img.png")
        self.assertEqual(M.safe_url("https://user:pw@h.example:8443/p?q=1#f"), "https://h.example:8443/p")


class ApiErrorTests(Base):
    def run_api_error(self, outcome):
        M.OPENER = FakeOpener(outcome)
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assertEqual(len(M.OPENER.requests), 1, "must send exactly one request")
        self.assert_clean(out + err)
        return err

    def test_http_error_json_echoing_key_and_signed_url(self):
        body = json.dumps({"error": {"code": "invalid_api_key", "type": "auth", "message": LEAKY_TEXT}}).encode()
        err = self.run_api_error(http_error(401, body))
        self.assertIn("HTTP 401", err)
        self.assertIn("invalid_api_key", err)
        self.assertIn("https://deeprouter.top/v1/images/generations", err)

    def test_http_error_with_large_b64_payload(self):
        body = json.dumps({"error": {"message": "bad", "b64_json": BIG_B64},
                           "data": [{"b64_json": BIG_B64}], "echo": LEAKY_TEXT}).encode()
        err = self.run_api_error(http_error(400, body))
        self.assertIn("message=bad", err)

    def test_http_error_string_error_field(self):
        body = json.dumps({"error": LEAKY_TEXT}).encode()
        self.run_api_error(http_error(403, body))

    def test_http_error_non_json_body(self):
        err = self.run_api_error(http_error(502, ("<html>" + LEAKY_TEXT + "</html>").encode()))
        self.assertIn("non-JSON", err)

    def test_redirect_is_refused_without_leaking_location(self):
        e = own(urllib.error.HTTPError(SIGNED_URL, 302, "redirect to " + SIGNED_URL, {"Location": SIGNED_URL},
                                       io.BytesIO(b"")))
        err = self.run_api_error(e)
        self.assertIn("redirect refused", err)

    def test_no_redirect_handler_message_is_safe(self):
        class Req(object):
            full_url = SIGNED_URL
        with self.assertRaises(urllib.error.HTTPError) as cm:
            M.NoRedirect().redirect_request(Req(), None, 302, "Found", {}, SIGNED_URL)
        own(cm.exception)
        self.assert_clean(str(cm.exception) + cm.exception.filename)

    def test_urlerror_reason_contains_key_and_signed_url(self):
        err = self.run_api_error(urllib.error.URLError(LEAKY_TEXT))
        self.assertIn("billed", err)

    def test_urlerror_timeout_reason(self):
        err = self.run_api_error(urllib.error.URLError(socket.timeout("timed out " + LEAKY_TEXT)))
        self.assertIn("timeout", err)

    def test_bare_timeout_error(self):
        err = self.run_api_error(TimeoutError("read timed out " + LEAKY_TEXT))
        self.assertIn("timeout", err)
        self.assertIn("not retrying", err)

    def test_oserror_and_ssl_error(self):
        self.run_api_error(OSError(LEAKY_TEXT))
        self.run_api_error(ssl.SSLError(1, LEAKY_TEXT))

    def test_unexpected_exception(self):
        self.run_api_error(ValueError(LEAKY_TEXT))
        self.run_api_error(RuntimeError(LEAKY_TEXT))

    def test_200_non_json_body(self):
        err = self.run_api_error(FakeResp(("not json " + LEAKY_TEXT).encode()))
        self.assertIn("not JSON", err)

    def test_200_json_without_image(self):
        for body in ({"data": []}, {"data": BIG_B64, "error": {"message": LEAKY_TEXT, "data": BIG_B64}},
                     {"message": LEAKY_TEXT, "b64_json": BIG_B64}, [BIG_B64, LEAKY_TEXT],
                     {"data": [{"b64_json": None, "revised": LEAKY_TEXT}]}):
            err = self.run_api_error(FakeResp(body))
            self.assertTrue("no image" in err or "no b64_json" in err, err)

    def test_invalid_base64(self):
        self.run_api_error(FakeResp({"data": [{"b64_json": "!!!" + LEAKY_TEXT}]}))


class DownloadErrorTests(Base):
    def test_download_errors_show_host_only(self):
        timeout = TimeoutError(LEAKY_TEXT)
        for excs in ([urllib.error.URLError(LEAKY_TEXT)], [timeout] * 3,
                     [http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL)], [ValueError(LEAKY_TEXT)]):
            M.OPENER = FakeOpener(FakeResp({"data": [{"url": SIGNED_URL}]}), *excs)
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertEqual(code, 1)
            self.assertEqual([r.get_method() for r in M.OPENER.requests], ["POST"] + ["GET"] * len(excs))
            # Each retry adds one bounded "note:" line.
            self.assert_clean(out + err, max_len=700 + 300 * (len(excs) - 1))
            self.assertIn("bucket.s3.amazonaws.com", err)
            self.assertIsNone(M.OPENER.requests[1].get_header("Authorization"))
            self.assertIn("recovery file: ", out)
        self.assertEqual(len(self.recovery_files()), 4)

    def test_non_https_url_refused(self):
        M.OPENER = FakeOpener(FakeResp({"data": [{"url": "http://evil.example/x.png?key=" + OTHER_KEY}]}))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assert_clean(out + err)
        self.assertEqual(os.listdir(self.tmp.name), [], "an unusable URL is not written to a recovery file")

    def test_unsafe_result_urls_refused_without_get(self):
        for url in ("https://localhost/x.png", "https://127.0.0.1/x.png", "https://[::1]/x.png",
                    "https://169.254.169.254/latest/meta-data", "https://10.1.2.3/x.png", "https://192.168.1.5/x",
                    "https://[fd00::1]/x", "https://[::ffff:127.0.0.1]/x", "https://0.0.0.0/x", "https://224.0.0.1/x",
                    "https://metadata.google.internal/computeMetadata/v1/", "https://metadata/x", "https://intranet/x",
                    "https://127.1/x", "https://2130706433/x", "https://0x7f.0x1/x", "https://user:pw@cdn.example.com/x",
                    "https://printer.local/x", "ftp://cdn.example.com/x", "file:///etc/passwd"):
            M.OPENER = FakeOpener(FakeResp({"data": [{"url": url}]}))
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertEqual(code, 1, url)
            self.assertEqual(len(M.OPENER.requests), 1, "no GET for %s" % url)
            self.assertIn("refusing", err, url)
        self.assertEqual(self.recovery_files(), [])

    def test_host_name_not_resolved_before_get(self):
        # The local DNS pre-check was removed by request: whatever the resolver would
        # answer, a host name that passes the static checks is fetched via OPENER.
        for addr in ("10.0.0.5", "127.0.0.1", "169.254.169.254", "fd00:ec2::254", "::1", "198.18.0.7"):
            self.dns_answer = addr
            M.OPENER = FakeOpener(FakeResp({"data": [{"url": SIGNED_URL}]}), FakeResp(PNG))
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertIsNone(code, err)
            self.assertEqual([r.get_method() for r in M.OPENER.requests], ["POST", "GET"], addr)
            self.assertNotIn("non-public", err)
            self.assert_clean(out + err)
        self.assertEqual(self.lookups, [], "the script did not resolve the host itself")


class RecoveryTests(Base):
    """Generate/edit succeeded, a result download failed: recovery file, then `recover`."""

    def failed_generate(self, body, *downloads):
        M.OPENER = FakeOpener(FakeResp(body), *downloads)
        code, out, err = self.capture(M.main, self.gen_args("--timeout", "42"))
        self.assertEqual(code, 1, err)
        posts = [r for r in M.OPENER.requests if r.get_method() == "POST"]
        self.assertEqual(len(posts), 1, "exactly one generate POST")
        self.assert_clean(out + err)
        recs = self.recovery_files()
        self.assertEqual(len(recs), 1)
        self.assertIn("recovery file: " + os.path.abspath(recs[0]), out)
        self.assertIn("was not resent", err)
        self.assertIn("recover --from", err)
        with open(recs[0], encoding="utf-8") as f:
            text = f.read()
        return recs[0], json.loads(text), text, out, err

    def assert_private(self, path):
        if os.name == "posix":
            self.assertEqual(stat.S_IMODE(os.stat(path).st_mode), 0o600)

    def recover(self, *args):
        return self.capture(M.main, ["recover"] + list(args))

    def test_403_writes_recovery_file_with_full_url(self):
        rec, doc, text, out, err = self.failed_generate({"data": [{"url": SIGNED_URL}]},
                                                        http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL))
        self.assertIn("download HTTP 403 from bucket.s3.amazonaws.com", err)
        self.assert_private(rec)
        self.assertEqual(doc["kind"], M.RECOVER_KIND)
        self.assertEqual(doc["timeout"], 42)
        error = doc["items"][0].pop("error")
        self.assertEqual(doc["items"], [{"index": 1, "url": SIGNED_URL, "stem": "deeprouter-generate-1",
                                         "fallback_ext": "png"}])
        self.assertRegex(error, r"\Adownload HTTP 403 from bucket\.s3\.amazonaws\.com \[\d+ ms; proxy none\]\Z")
        self.assertEqual(self.sleeps, [], "403 is not retried")
        for bad in (FAKE_KEY, OTHER_KEY, "Bearer", "b64_json", "Authorization", "hunter2pass"):
            self.assertNotIn(bad, text)
        self.assertIsNone(M.OPENER.requests[1].get_header("Authorization"))
        self.assertEqual([n for n in os.listdir(self.tmp.name) if not n.startswith(M.RECOVER_PREFIX)], [])

    def test_edit_download_failure_writes_recovery_file(self):
        img = os.path.join(self.tmp.name, "in.png")
        with open(img, "wb") as f:
            f.write(PNG)
        out_dir = os.path.join(self.tmp.name, "new", "out")
        M.OPENER = FakeOpener(FakeResp({"data": {"url": SIGNED_URL}}), http_error(403, b"", url=SIGNED_URL))
        code, out, err = self.capture(M.main, ["edit", "--confirm", "--prompt", "p", "--image", img, "--out-dir", out_dir])
        self.assertEqual(code, 1)
        self.assertEqual(len(M.OPENER.requests), 2)
        self.assertIn("the edit request succeeded", err)
        self.assert_clean(out + err)
        recs = self.recovery_files(out_dir)
        self.assertEqual(len(recs), 1)
        self.assert_private(recs[0])
        if os.name == "posix":
            for d in (out_dir, os.path.dirname(out_dir)):
                self.assertEqual(stat.S_IMODE(os.stat(d).st_mode), 0o700, "created dirs are 0700")

    def test_existing_out_dir_mode_untouched(self):
        if os.name != "posix":
            self.skipTest("POSIX modes")
        os.chmod(self.tmp.name, 0o755)
        self.failed_generate({"data": [{"url": SIGNED_URL}]}, http_error(403, b"", url=SIGNED_URL))
        self.assertEqual(stat.S_IMODE(os.stat(self.tmp.name).st_mode), 0o755)

    def test_partial_save_keeps_image_and_records_only_failed_url(self):
        existing = os.path.join(self.tmp.name, "deeprouter-generate-1.png")
        with open(existing, "wb") as f:
            f.write(b"user file")
        body = {"data": [{"b64_json": base64.b64encode(PNG).decode()}, {"url": SIGNED_URL}]}
        rec, doc, text, out, err = self.failed_generate(body, http_error(403, b"", url=SIGNED_URL))
        with open(existing, "rb") as f:
            self.assertEqual(f.read(), b"user file", "existing file not overwritten")
        kept = os.path.join(self.tmp.name, "deeprouter-generate-1-2.png")
        with open(kept, "rb") as f:
            self.assertEqual(f.read(), PNG)
        self.assertIn("saved " + os.path.abspath(kept), out)
        self.assertEqual([it["url"] for it in doc["items"]], [SIGNED_URL])
        self.assertEqual(doc["items"][0]["index"], 2)
        self.assertNotIn(body["data"][0]["b64_json"][:40], text)
        self.assertIn("1 of 1 result download", err)

    def test_recover_success_no_post_no_auth_key_optional(self):
        rec, _, _, _, _ = self.failed_generate({"data": [{"url": SIGNED_URL}]}, http_error(403, b"", url=SIGNED_URL))
        with open(rec, "rb") as f:
            before = f.read()
        for key in (None, FAKE_KEY):
            if key is None:
                os.environ.pop("DEEPROUTER_API_KEY", None)
            else:
                os.environ["DEEPROUTER_API_KEY"] = key
            M.OPENER = FakeOpener(FakeResp(PNG))
            code, out, err = self.recover("--from", rec)
            self.assertIsNone(code, err)
            self.assertEqual(len(M.OPENER.requests), 1)
            req = M.OPENER.requests[0]
            self.assertEqual(req.get_method(), "GET")
            self.assertEqual(req.full_url, SIGNED_URL)
            self.assertIn("X-Amz-Signature=", req.selector, "signed query is sent in the GET")
            for h in ("Authorization", "Cookie", "Referer"):
                self.assertIsNone(req.get_header(h))
            self.assertNotIn(FAKE_KEY, str(req.header_items()))
            self.assert_clean(out + err)
            self.assertIn("still contains the complete result URLs", out)
        self.assertTrue(os.path.exists(os.path.join(self.tmp.name, "deeprouter-generate-1.png")))
        self.assertTrue(os.path.exists(os.path.join(self.tmp.name, "deeprouter-generate-1-2.png")), "second run: new name")
        with open(rec, "rb") as f:
            self.assertEqual(f.read(), before, "recovery file left unchanged")

    def test_recover_reads_neither_config_nor_key(self):
        rec, _, _, _, _ = self.failed_generate({"data": [{"url": SIGNED_URL}]}, http_error(403, b"", url=SIGNED_URL))
        # A config that would stop generate, a half-set env pair, and a guard
        # that fails if recover even looks for the config.
        d = os.path.join(self.home.name, ".deeprouter")
        os.makedirs(d)
        with open(os.path.join(d, "image-skill.json"), "w") as f:
            f.write("{not json")
        os.environ["DEEPROUTER_IMAGE_BASE_URL"] = "https://env.example.com"
        os.environ.pop("DEEPROUTER_API_KEY", None)
        saved = (M.config_path, M.read_image_config, M.resolve_api)

        def boom(*a, **k):
            raise AssertionError("recover touched the API config")
        M.config_path = M.read_image_config = M.resolve_api = boom
        try:
            M.OPENER = FakeOpener(FakeResp(PNG))
            code, out, err = self.capture(M.main, ["recover", "--from", rec])
        finally:
            M.config_path, M.read_image_config, M.resolve_api = saved
        self.assertIsNone(code, err)
        req = M.OPENER.requests[0]
        self.assertEqual(req.get_method(), "GET")
        self.assertIsNone(req.get_header("Authorization"))
        self.assert_clean(out + err)

    def test_recover_does_not_overwrite_and_honors_out_dir(self):
        rec, _, _, _, _ = self.failed_generate({"data": [{"url": SIGNED_URL}]}, http_error(403, b"", url=SIGNED_URL))
        out_dir = os.path.join(self.tmp.name, "o")
        os.mkdir(out_dir)
        taken = os.path.join(out_dir, "deeprouter-generate-1.png")
        with open(taken, "wb") as f:
            f.write(b"mine")
        M.OPENER = FakeOpener(FakeResp(PNG))
        code, out, err = self.recover("--from", rec, "--out-dir", out_dir, "--timeout", "5")
        self.assertIsNone(code, err)
        with open(taken, "rb") as f:
            self.assertEqual(f.read(), b"mine")
        self.assertEqual(sorted(os.listdir(out_dir)), ["deeprouter-generate-1-2.png", "deeprouter-generate-1.png"])
        M.OPENER = NeverOpener()
        code, out, err = self.recover("--from", rec, "--out-dir", os.path.join(self.tmp.name, "missing"))
        self.assertEqual(code, 2)

    def test_recover_failure_does_not_post_or_retry(self):
        rec, _, _, _, _ = self.failed_generate({"data": [{"url": SIGNED_URL}]}, http_error(403, b"", url=SIGNED_URL))
        with open(rec, "rb") as f:
            before = f.read()
        for excs in ([http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL)], [TimeoutError(LEAKY_TEXT)] * 3):
            M.OPENER = FakeOpener(*excs)
            code, out, err = self.recover("--from", rec)
            self.assertEqual(code, 1)
            # 403: one GET. Timeout: transient, so at most three GETs; never a POST.
            self.assertEqual([r.get_method() for r in M.OPENER.requests], ["GET"] * len(excs))
            self.assert_clean(out + err)
            self.assertIn("original recovery file was left unchanged", err)
        self.assertEqual(len(self.recovery_files()), 1, "no new recovery file when nothing succeeded")
        with open(rec, "rb") as f:
            self.assertEqual(f.read(), before)

    def test_recover_partial_writes_new_file_for_remaining(self):
        other = SIGNED_URL.replace("img.png", "img2.png")
        rec = self.write_rec({"kind": M.RECOVER_KIND, "version": 1, "items": [
            {"index": 1, "url": SIGNED_URL, "stem": "a"}, {"index": 2, "url": other, "stem": "b"}]})
        M.OPENER = FakeOpener(FakeResp(PNG), http_error(403, b"", url=other))
        code, out, err = self.recover("--from", rec)
        self.assertEqual(code, 1)
        self.assert_clean(out + err)
        new = [p for p in self.recovery_files() if p != rec]
        self.assertEqual(len(new), 1)
        self.assertIn("recovery file (remaining downloads): " + new[0], out)
        self.assert_private(new[0])
        with open(new[0], encoding="utf-8") as f:
            self.assertEqual([it["url"] for it in json.load(f)["items"]], [other])
        self.assertTrue(os.path.exists(os.path.join(self.tmp.name, "a.png")))

    def write_rec(self, doc, name="deeprouter-download-recover-test.json"):
        path = os.path.join(self.tmp.name, name)
        with open(path, "w", encoding="utf-8") as f:
            f.write(doc if isinstance(doc, str) else json.dumps(doc))
        return path

    def test_untrusted_recovery_files_rejected(self):
        def doc(**item):
            it = {"index": 1, "url": SIGNED_URL, "stem": "x"}
            it.update(item)
            return {"kind": M.RECOVER_KIND, "version": 1, "items": [it]}
        cases = [
            "not json " + OTHER_KEY, "[]", '{"kind": "%s", "version": 1, "items": [], "items": []}' % M.RECOVER_KIND,
            {"kind": "other", "version": 1, "items": [{"url": SIGNED_URL}]},
            {"kind": M.RECOVER_KIND, "version": 2, "items": [{"url": SIGNED_URL}]},
            {"kind": M.RECOVER_KIND, "version": 1, "items": []},
            {"kind": M.RECOVER_KIND, "version": 1, "items": {"url": SIGNED_URL}},
            {"kind": M.RECOVER_KIND, "version": 1, "items": [SIGNED_URL]},
            {"kind": M.RECOVER_KIND, "version": 1, "items": [{"stem": "x"}]},
            {"kind": M.RECOVER_KIND, "version": 1, "timeout": True, "items": [{"url": SIGNED_URL}]},
            '{"kind": "%s", "version": 1, "timeout": NaN, "items": [{"url": "%s"}]}' % (M.RECOVER_KIND, SIGNED_URL),
            {"kind": M.RECOVER_KIND, "version": 1, "api_key": OTHER_KEY, "items": [{"url": SIGNED_URL}]},
            doc(Authorization="Bearer " + FAKE_KEY), doc(headers={"Cookie": "a=b"}), doc(b64_json=BIG_B64),
            doc(url=5), doc(url="http://cdn.example.com/x.png?key=" + OTHER_KEY), doc(url="file:///etc/passwd"),
            doc(url="https://localhost/x"), doc(url="https://127.0.0.1/x"), doc(url="https://10.0.0.1/x"),
            doc(url="https://169.254.169.254/latest/meta-data/"), doc(url="https://metadata.google.internal/x"),
            doc(url="https://[fe80::1]/x"), doc(url="https://user:" + OTHER_KEY + "@cdn.example.com/x"),
            doc(stem="../../etc/x"), doc(stem="a/b"), doc(stem="..\\x"), doc(stem="/abs"), doc(stem=".hidden"),
            doc(stem=""), doc(stem="x" * 200), doc(fallback_ext="sh"), doc(fallback_ext="../png"), doc(index=True),
            doc(error=5),
        ]
        for c in cases:
            M.OPENER = NeverOpener()
            rec = self.write_rec(c)
            code, out, err = self.recover("--from", rec)
            self.assertEqual(code, 2, "should reject %r: %r" % (str(c)[:80], err))
            self.assertIn("recovery file rejected", err)
            self.assert_clean(out + err)
            self.assertNotIn("Cookie", err)
        self.assertEqual(sorted(os.listdir(self.tmp.name)), ["deeprouter-download-recover-test.json"])

    def test_oversized_and_non_regular_recovery_files_rejected(self):
        big = self.write_rec(" " * (M.MAX_RECOVER_BYTES + 1))
        for path in (big, self.tmp.name, os.path.join(self.tmp.name, "nope.json")):
            code, out, err = self.recover("--from", path)
            self.assertEqual(code, 2, path)
            self.assertIn("recovery file rejected", err)

    def test_symlinked_recovery_file_rejected(self):
        real = self.write_rec({"kind": M.RECOVER_KIND, "version": 1, "items": [{"url": SIGNED_URL}]})
        link = os.path.join(self.tmp.name, "link.json")
        try:
            os.symlink(real, link)
        except (OSError, NotImplementedError):
            self.skipTest("symlinks not available")
        code, out, err = self.recover("--from", link)
        self.assertEqual(code, 2)
        self.assertIn("symbolic link", err)

    def test_recover_host_name_not_resolved_before_get(self):
        rec = self.write_rec({"kind": M.RECOVER_KIND, "version": 1, "items": [{"url": SIGNED_URL}]})
        self.dns_answer = "169.254.169.254"
        M.OPENER = FakeOpener(FakeResp(PNG))
        code, out, err = self.recover("--from", rec)
        self.assertIn(code, (None, 0), err)
        self.assertEqual([r.get_method() for r in M.OPENER.requests], ["GET"])
        self.assertNotIn("non-public", err)
        self.assert_clean(out + err)
        self.assertEqual(self.lookups, [])

    def test_recover_does_not_need_confirm_and_has_no_api_options(self):
        rec = self.write_rec({"kind": M.RECOVER_KIND, "version": 1, "items": [{"url": SIGNED_URL}]})
        for extra in (["--confirm"], ["--host", "deeprouter.top"], ["--prompt", "x"]):
            code, out, err = self.recover("--from", rec, *extra)
            self.assertEqual(code, 2, extra)


def headers(**kv):
    """Case-insensitive response headers, like the ones urllib gives an HTTPError."""
    h = email.message.Message()
    for k, v in kv.items():
        h[k.replace("_", "-")] = v
    return h


BODY_MARKER = "BODY-MARKER-3f9a"  # only in response bodies; must never be printed or stored
DIAG_403_HEADERS = dict(Content_Type="text/html; charset=UTF-8", Server="cloudflare", CF_Ray="8c1f2a3b4c5d6e7f-LAX",
                        Set_Cookie="__cf_bm=abcSECRETtok; path=/", WWW_Authenticate='Bearer realm="x", error="hunter2pass"',
                        X_Amz_Request_Id="REQIDNOTALLOWLISTED", Location=SIGNED_URL)


def diag_error(code, reason="Forbidden", url=SIGNED_URL, **hdrs):
    body = ("<html>%s %s</html>" % (BODY_MARKER, LEAKY_TEXT)).encode()
    return own(urllib.error.HTTPError(url, code, reason, headers(**hdrs), io.BytesIO(body)))


class DownloadDiagnosticsTests(Base):
    """A failed result download is reported with redacted clues; only transient failures are retried."""

    URL_BODY = {"data": [{"url": SIGNED_URL}]}

    def generate(self, *downloads):
        M.OPENER = FakeOpener(FakeResp(self.URL_BODY), *downloads)
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(M.OPENER.outcomes, [], "every planned outcome used")
        return code, out, err

    def methods(self):
        return [r.get_method() for r in M.OPENER.requests]

    def recovery_text(self):
        recs = self.recovery_files()
        self.assertEqual(len(recs), 1)
        with open(recs[0], encoding="utf-8") as f:
            return f.read()

    def assert_no_secret_diag(self, text):
        for bad in (SIGNED_URL, "X-Amz-Signature", BODY_MARKER, "Set-Cookie", "__cf_bm", "abcSECRETtok", "realm",
                    "hunter2pass", "charset", "REQIDNOTALLOWLISTED", "X-Amz-Request-Id", "Location", FAKE_KEY):
            self.assertNotIn(bad, text)

    def test_403_diagnostic_redacted_and_not_retried(self):
        code, out, err = self.generate(diag_error(403, **DIAG_403_HEADERS))
        self.assertEqual(code, 1)
        self.assertEqual(self.methods(), ["POST", "GET"], "no second generate, no second GET")
        self.assertEqual(self.sleeps, [])
        self.assertNotIn("retrying", err)
        self.assertIn("was not resent", err)
        self.assertIn("never after HTTP 403", err)
        line = [l for l in err.splitlines() if l.startswith("error: image 1:")][0]
        self.assertRegex(line, r"download HTTP 403 from bucket\.s3\.amazonaws\.com \[\d+ ms; proxy none; "
                               r"reason Forbidden; Content-Type: text/html; Server: cloudflare; "
                               r"CF-Ray: 8c1f2a3b4c5d6e7f-LAX; WWW-Authenticate: Bearer\]")
        self.assert_no_secret_diag(out + err)
        self.assert_clean(out + err)
        text = self.recovery_text()
        doc = json.loads(text)
        error = doc["items"][0]["error"]
        self.assertIn("download HTTP 403 from bucket.s3.amazonaws.com", error)
        self.assertRegex(error, r"\[\d+ ms; proxy none; reason Forbidden; Content-Type: text/html; Server: cloudflare")
        self.assertLessEqual(len(error), 300)
        # The full URL is stored only as the item's url, never inside the error text.
        self.assert_no_secret_diag(error)
        self.assert_no_secret_diag(text.replace(json.dumps(SIGNED_URL), '"<url>"'))

    def test_leaky_reason_phrase_not_shown(self):
        code, out, err = self.generate(http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL))
        self.assertEqual(code, 1)
        self.assertIn("download HTTP 403 from bucket.s3.amazonaws.com", err)
        self.assertNotIn("reason", err.split("download HTTP 403", 1)[1].split("]", 1)[0])
        self.assertNotIn("upstream said", err)
        self.assert_clean(out + err)

    def test_non_transient_failures_not_retried(self):
        refused = urllib.error.URLError(ConnectionRefusedError(111, "Connection refused"))
        for i, outcome in enumerate((diag_error(403), diag_error(404, "Not Found"), diag_error(500, "Internal"),
                                     diag_error(401, "Unauthorized"), refused, ssl.SSLError(1, "bad cert"))):
            os.makedirs(os.path.join(self.tmp.name, str(i)))
            M.OPENER = FakeOpener(FakeResp(self.URL_BODY), outcome)
            code, out, err = self.capture(M.main, self.gen_args("--out-dir", os.path.join(self.tmp.name, str(i))))
            self.assertEqual(code, 1, err)
            self.assertEqual(self.methods(), ["POST", "GET"], repr(outcome))
            self.assertRegex(err, r"\[\d+ ms; proxy none")
            self.assert_clean(out + err)
        self.assertEqual(self.sleeps, [])

    def test_transient_status_retried_then_saved(self):
        for status in (429, 502, 503, 504):
            M.OPENER = FakeOpener(FakeResp(self.URL_BODY), diag_error(status, "Busy", Retry_After="2"), FakeResp(PNG))
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertIsNone(code, err)
            self.assertEqual(self.methods(), ["POST", "GET", "GET"], status)
            self.assertEqual(M.OPENER.requests[1].full_url, M.OPENER.requests[2].full_url)
            self.assertIn("note: image 1: download HTTP %d from bucket.s3.amazonaws.com" % status, err)
            self.assertIn("Retry-After: 2", err)
            self.assertIn("retrying only the download in 1s (attempt 2 of 3)", err)
            self.assertNotIn("error:", err)
            self.assert_clean(out + err)
        self.assertEqual(self.sleeps, [1, 1, 1, 1])
        self.assertEqual(self.recovery_files(), [])
        self.assertEqual(len([n for n in os.listdir(self.tmp.name) if n.endswith(".png")]), 4)

    def test_timeout_and_reset_retried(self):
        for exc in (TimeoutError("read timed out"), socket.timeout("timed out"),
                    urllib.error.URLError(socket.timeout("timed out")),
                    urllib.error.URLError(ConnectionResetError(104, "Connection reset by peer")),
                    ConnectionResetError(104, "reset")):
            self.sleeps.clear()
            M.OPENER = FakeOpener(FakeResp(self.URL_BODY), exc, FakeResp(PNG))
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertIsNone(code, err)
            self.assertEqual(self.methods(), ["POST", "GET", "GET"], repr(exc))
            self.assertEqual(self.sleeps, [1])
            self.assert_clean(out + err)

    def test_transient_retries_are_capped_then_recovery_file(self):
        code, out, err = self.generate(diag_error(503, "Service Unavailable"), diag_error(503, "Service Unavailable"),
                                       diag_error(503, "Service Unavailable"))
        self.assertEqual(code, 1)
        self.assertEqual(self.methods(), ["POST", "GET", "GET", "GET"])
        self.assertEqual(self.sleeps, list(M.RETRY_DELAYS))
        self.assertIn("gave up after 3 attempts", err)
        self.assert_clean(out + err, max_len=1300)
        error = json.loads(self.recovery_text())["items"][0]["error"]
        self.assertIn("download HTTP 503 from bucket.s3.amazonaws.com", error)
        self.assertIn("gave up after 3 attempts", error)
        # The stored diagnostic still passes recover's own checks.
        M.OPENER = FakeOpener(FakeResp(PNG))
        code, out, err = self.capture(M.main, ["recover", "--from", self.recovery_files()[0]])
        self.assertIsNone(code, err)

    def test_generate_api_errors_still_never_retried(self):
        for outcome in (http_error(503, b"{}"), http_error(429, b"{}"), TimeoutError("t"),
                        urllib.error.URLError(ConnectionResetError(104, "reset"))):
            M.OPENER = FakeOpener(outcome)
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertEqual(code, 1)
            self.assertEqual(self.methods(), ["POST"], repr(outcome))
        self.assertEqual(self.sleeps, [])

    def write_rec(self, urls):
        path = os.path.join(self.tmp.name, "deeprouter-download-recover-test.json")
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"kind": M.RECOVER_KIND, "version": 1,
                       "items": [{"index": i, "url": u, "stem": "img-%d" % i} for i, u in enumerate(urls, 1)]}, f)
        return path

    def assert_download_only(self, reqs):
        for r in reqs:
            self.assertEqual(r.get_method(), "GET")
            self.assertIsNone(r.data)
            self.assertNotIn("/images/generations", r.full_url)
            self.assertNotIn("/images/edits", r.full_url)
            for h in ("Authorization", "Cookie", "Referer", "Proxy-authorization"):
                self.assertIsNone(r.get_header(h), h)
            for k in (FAKE_KEY, IMG_ENV_KEY, FILE_KEY):
                self.assertNotIn(k, str(r.header_items()))

    def test_recover_download_only_with_keys_in_env(self):
        rec = self.write_rec([SIGNED_URL])
        os.environ["DEEPROUTER_IMAGE_BASE_URL"] = "https://img.example.com/v1"
        os.environ["DEEPROUTER_IMAGE_API_KEY"] = IMG_ENV_KEY
        for outcomes, want_code in (([diag_error(403, **DIAG_403_HEADERS)], 1),
                                    ([diag_error(503, "Busy"), diag_error(403)], 1),
                                    ([diag_error(502, "Bad Gateway"), FakeResp(PNG)], None)):
            M.OPENER = FakeOpener(*outcomes)
            code, out, err = self.capture(M.main, ["recover", "--from", rec])
            self.assertEqual(code, want_code, err)
            self.assertEqual(M.OPENER.outcomes, [])
            self.assertEqual(len(M.OPENER.requests), len(outcomes))
            self.assert_download_only(M.OPENER.requests)
            self.assert_no_secret_diag(out + err)
            self.assert_clean(out + err)
            if want_code:
                self.assertIn("download HTTP 403 from bucket.s3.amazonaws.com", err)
                self.assertIn("Nothing was sent to the image API", err)

    def test_write_recovery_is_strict_json_and_round_trips(self):
        err = "download HTTP 403 from download.example.cn [812 ms; proxy none; reason Forbidden; Server: nginx]"
        items = [{"index": 1, "url": SIGNED_URL, "stem": "deeprouter-generate-1", "fallback_ext": "gif", "error": err}]
        path = M.write_recovery(self.tmp.name, items, 600)
        if os.name == "posix":
            self.assertEqual(stat.S_IMODE(os.stat(path).st_mode), 0o600)
        with open(path, "rb") as f:
            raw = f.read()
        doc = json.loads(raw.decode("utf-8"), object_pairs_hook=M._reject_dup_keys, parse_constant=M._reject_constant)
        self.assertEqual(set(doc["items"][0]), {"index", "url", "stem", "fallback_ext", "error"})
        self.assertIn(b'"fallback_ext": "gif"', raw)
        self.assertEqual(M.load_recovery(path), ([{"index": 1, "url": SIGNED_URL, "stem": "deeprouter-generate-1",
                                                    "fallback_ext": "gif"}], 600))
        # A file damaged like the one reported (a newline inside a key) is rejected without a request.
        bad = os.path.join(self.tmp.name, "deeprouter-download-recover-broken.json")
        with open(bad, "wb") as f:
            f.write(raw.replace(b'"fallback_ext"', b'"fallback\n_ext"'))
        M.OPENER = NeverOpener()
        code, out, err = self.capture(M.main, ["recover", "--from", bad])
        self.assertEqual(code, 2)
        self.assertIn("not valid JSON", err)
        self.assert_clean(out + err)

    def test_headers_allowlist(self):
        got = M.safe_headers(headers(Content_Type="image/gif", Server="x" * 81, Via="1.1 varnish", X_Cache="MISS",
                                     CF_Cache_Status="DYNAMIC", Set_Cookie="a=b", Authorization="Bearer " + FAKE_KEY,
                                     WWW_Authenticate=FAKE_KEY + " realm=x", Content_Length="13"))
        self.assertEqual(got, ["Content-Type: image/gif", "Via: 1.1 varnish", "X-Cache: MISS",
                               "CF-Cache-Status: DYNAMIC", "Content-Length: 13"])
        self.assertEqual(M.safe_headers({"server": "a\x00b", "content-type": "text/plain"}), ["Content-Type: text/plain"])
        self.assertEqual(M.safe_headers(None), [])


class ResourceWarningTests(Base):
    """ResourceWarnings stay on while product output is checked, and would be caught."""

    def test_capture_reports_unclosed_resources(self):
        marker = os.path.join(self.tmp.name, "canary-unclosed")
        with open(marker, "wb"):
            pass

        def leak():
            open(marker, "rb")  # deliberately never closed

        _, out, err = self.capture(leak)
        self.assertIn("ResourceWarning", err)
        self.assertIn("canary-unclosed", err)

    @unittest.skipUnless(http_error_warns_when_unclosed(), "this Python does not warn about unclosed HTTPError")
    def test_unclosed_leaky_http_error_fails_assert_clean(self):
        def leak():
            urllib.error.HTTPError(SIGNED_URL, 401, LEAKY_TEXT, {}, io.BytesIO(b""))  # never closed

        _, out, err = self.capture(leak)
        self.assertIn("ResourceWarning", err)
        with self.assertRaises(AssertionError):
            self.assert_clean(out + err)

    def test_error_paths_leave_nothing_unclosed(self):
        # API error, refused redirect, failed download (recovery file), failed recover.
        for outcomes in ((http_error(401, json.dumps({"error": {"message": LEAKY_TEXT}}).encode()),),
                         (own(urllib.error.HTTPError(M.safe_url(SIGNED_URL), 302, "redirect refused", {}, None)),),
                         (FakeResp({"data": [{"url": SIGNED_URL}]}), http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL))):
            M.OPENER = FakeOpener(*outcomes)
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertEqual(code, 1)
            self.assertNotIn("ResourceWarning", out + err)
            self.assert_clean(out + err)
        rec = self.recovery_files()[0]
        M.OPENER = FakeOpener(http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL))
        code, out, err = self.capture(M.main, ["recover", "--from", rec])
        self.assertEqual(code, 1)
        self.assertNotIn("ResourceWarning", out + err)
        self.assert_clean(out + err)


class ExtraJsonTests(Base):
    def assert_rejected(self, extra, *more):
        M.OPENER = NeverOpener()
        code, out, err = self.capture(M.main, self.gen_args("--extra-json", json.dumps(extra), *more))
        self.assertEqual(code, 2, "should reject %r; stderr=%r" % (extra, err))
        self.assert_clean(out + err)
        return err

    def test_n_cannot_be_bypassed(self):
        for n in (0, 11, 100, -1, "3", 2.0, True, None, [1]):
            self.assert_rejected({"n": n})

    def test_model_and_prompt_revalidated(self):
        for extra in ({"model": ""}, {"model": "  "}, {"model": None}, {"model": 5},
                      {"prompt": ""}, {"prompt": None}, {"prompt": ["x"]}):
            self.assert_rejected(extra)

    def test_credential_keys_rejected(self):
        for k in ("Authorization", "authorization", "AUTHORIZATION", "api_key", "API_KEY", "api-key", "Api-Key",
                  "apiKey", "x-api-key", "key", "KEY", "token", "Token", "access_token", "Access-Token",
                  "secret", "client_secret", "password", "DEEPROUTER_API_KEY"):
            err = self.assert_rejected({k: OTHER_KEY})
            self.assertIn("credential", err)
        self.assert_rejected({"svg": {"model": "m", "Authorization": "Bearer " + OTHER_KEY}})
        self.assert_rejected({"list": [{"api_key": OTHER_KEY}]})

    def test_bad_json_rejected(self):
        for raw in ("{not json " + OTHER_KEY, "[1,2]", '"s"'):
            M.OPENER = NeverOpener()
            code, out, err = self.capture(M.main, self.gen_args("--extra-json", raw))
            self.assertEqual(code, 2)
            self.assert_clean(out + err)

    def test_valid_merge_is_sent(self):
        M.OPENER = FakeOpener(FakeResp({"data": [{"b64_json": base64.b64encode(SVG).decode()}]}))
        extra = {"model": "gpt-image-2.6-flare", "output_format": "svg", "svg": {"model": "gpt-6-luna"}, "n": 2}
        code, out, err = self.capture(M.main, self.gen_args("--extra-json", json.dumps(extra)))
        self.assertIsNone(code, err)
        sent = json.loads(M.OPENER.requests[0].data.decode())
        self.assertEqual(sent["model"], "gpt-image-2.6-flare")
        self.assertEqual(sent["n"], 2)
        self.assertEqual(sent["svg"], {"model": "gpt-6-luna"})
        self.assertEqual(M.OPENER.requests[0].get_header("Authorization"), "Bearer " + FAKE_KEY)
        self.assertIn("SVG saved as untrusted", out)

    def test_no_confirm_not_sent(self):
        M.OPENER = NeverOpener()
        args = [a for a in self.gen_args() if a != "--confirm"]
        code, out, err = self.capture(M.main, args)
        self.assertEqual(code, 2)
        self.assertIn("not sent", err)


class ExtensionTests(Base):
    DANGEROUS = ("../../etc/passwd", "png/../../x", "..", "/abs", "exe", "png\x00.sh", "svg ", "p ng",
                 "html", "PNG.exe", "", 5, None, ["png"], {"a": 1})

    def test_safe_ext_allowlist(self):
        for v in self.DANGEROUS:
            if v == "svg ":
                continue  # stripped -> svg, still safe
            self.assertEqual(M.safe_ext(v), "png", repr(v))
        self.assertEqual(M.safe_ext("JPEG"), "jpg")
        self.assertEqual(M.safe_ext("svg"), "svg")
        self.assertEqual(M.safe_ext("gif"), "gif")
        self.assertEqual(M.safe_ext("webp"), "webp")

    def test_fallback_ext_for_body(self):
        self.assertEqual(M.fallback_ext_for({"format": "jpeg"}), "jpg")
        self.assertEqual(M.fallback_ext_for({"output_format": "gif"}), "gif")
        self.assertEqual(M.fallback_ext_for({"format": "../x", "output_format": "svg"}), "svg")
        for v in self.DANGEROUS:
            if v == "svg ":
                continue
            self.assertEqual(M.fallback_ext_for({"format": v, "output_format": v}), "png", repr(v))

    def test_dangerous_output_format_never_becomes_a_path(self):
        unknown = b"\x00\x01not-an-image"
        for v in ("../../../../tmp/pwned", "x/../../y", "sh", "png;rm -rf"):
            out_dir = tempfile.mkdtemp(dir=self.tmp.name)
            M.OPENER = FakeOpener(FakeResp({"data": [{"b64_json": base64.b64encode(unknown).decode()}]}))
            code, out, err = self.capture(M.main, ["generate", "--confirm", "--prompt", "p", "--out-dir", out_dir,
                                                   "--extra-json", json.dumps({"output_format": v})])
            self.assertIsNone(code, err)
            self.assertEqual(os.listdir(out_dir), ["deeprouter-generate-1.png"])
            # The random temp dir name may contain "sh"; check only what the script chose.
            self.assertNotIn(v, out.replace(out_dir, "<out>"))


class SaveTests(Base):
    def test_object_data(self):
        M.OPENER = FakeOpener(FakeResp({"data": {"b64_json": base64.b64encode(PNG).decode()}, "size": "1024x1024"}))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertIsNone(code, err)
        self.assertTrue(os.path.exists(os.path.join(self.tmp.name, "deeprouter-generate-1.png")))
        self.assertIn("metadata", out)
        self.assert_clean(out + err)

    def test_array_data_and_unique_names(self):
        body = {"data": [{"b64_json": base64.b64encode(PNG).decode()},
                         {"b64_json": base64.b64encode(b"\xff\xd8\xff\xe0jpg").decode()}]}
        for _ in range(2):
            M.OPENER = FakeOpener(FakeResp(body))
            code, out, err = self.capture(M.main, self.gen_args("--n", "2"))
            self.assertIsNone(code, err)
            self.assertNotIn(body["data"][0]["b64_json"][:20], out)
        self.assertEqual(sorted(os.listdir(self.tmp.name)), [
            "deeprouter-generate-1-2.png", "deeprouter-generate-1.png",
            "deeprouter-generate-2-2.jpg", "deeprouter-generate-2.jpg"])
        with open(os.path.join(self.tmp.name, "deeprouter-generate-1.png"), "rb") as f:
            self.assertEqual(f.read(), PNG)

    def test_url_item_downloaded_without_auth(self):
        M.OPENER = FakeOpener(FakeResp({"data": [{"url": SIGNED_URL}]}), FakeResp(PNG))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertIsNone(code, err)
        self.assertIsNone(M.OPENER.requests[1].get_header("Authorization"))
        self.assertEqual(os.listdir(self.tmp.name), ["deeprouter-generate-1.png"])
        self.assert_clean(out + err)

    def test_svg_note(self):
        M.OPENER = FakeOpener(FakeResp({"data": [{"b64_json": base64.b64encode(SVG).decode()}]}))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertIsNone(code, err)
        self.assertEqual(os.listdir(self.tmp.name), ["deeprouter-generate-1.svg"])
        self.assertIn("SVG saved as untrusted data", out)

    def test_metadata_is_redacted(self):
        M.OPENER = FakeOpener(FakeResp({"data": [{"b64_json": base64.b64encode(PNG).decode()}],
                                        "quality": LEAKY_TEXT, "size": BIG_B64}))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertIsNone(code, err)
        self.assert_clean(out + err)

    def test_edit_multipart(self):
        img = os.path.join(self.tmp.name, "in.png")
        with open(img, "wb") as f:
            f.write(PNG)
        out_dir = os.path.join(self.tmp.name, "out")
        M.OPENER = FakeOpener(FakeResp({"data": {"b64_json": base64.b64encode(PNG).decode()}}))
        code, out, err = self.capture(M.main, ["edit", "--confirm", "--prompt", "night", "--image", img,
                                               "--out-dir", out_dir])
        self.assertIsNone(code, err)
        req = M.OPENER.requests[0]
        self.assertTrue(req.full_url.endswith("/v1/images/edits"))
        self.assertNotIn("/v1/v1", req.full_url)
        self.assertEqual(os.listdir(out_dir), ["deeprouter-edit-1.png"])


class ApiSourceTests(Base):
    """Address/key resolution: env pair, then the image config file, then DEEPROUTER_API_KEY."""

    OK = {"data": [{"b64_json": base64.b64encode(PNG).decode()}]}

    def write_config(self, doc, raw=None):
        d = os.path.join(self.home.name, ".deeprouter")
        os.makedirs(d, mode=0o700, exist_ok=True)
        path = os.path.join(d, "image-skill.json")
        with open(path, "w") as f:
            f.write(raw if raw is not None else json.dumps(doc))
        os.chmod(path, 0o600)
        return path

    def file_config(self, base="https://img.example.com", key=FILE_KEY):
        return self.write_config({"version": 1, "provider": "custom", "base_url": base, "api_key": key})

    def generate_ok(self, *extra):
        M.OPENER = FakeOpener(FakeResp(self.OK))
        code, out, err = self.capture(M.main, self.gen_args(*extra))
        self.assertIsNone(code, err)
        self.assertEqual(len(M.OPENER.requests), 1)
        self.assert_clean(out + err)
        return M.OPENER.requests[0]

    def assert_refused(self, args, want, code=2):
        M.OPENER = NeverOpener()
        got, out, err = self.capture(M.main, args)
        self.assertEqual(got, code, err)
        self.assertIn(want, err)
        self.assert_clean(out + err)
        return err

    def test_config_path_is_under_home(self):
        self.assertEqual(M.config_path(), os.path.join(self.home.name, ".deeprouter", "image-skill.json"))

    def test_legacy_env_only_uses_official_host(self):
        req = self.generate_ok()
        self.assertEqual(req.full_url, "https://deeprouter.top/v1/images/generations")
        self.assertEqual(req.get_header("Authorization"), "Bearer " + FAKE_KEY)
        req = self.generate_ok("--host", "www.henapi.top")
        self.assertEqual(req.full_url, "https://www.henapi.top/v1/images/generations")

    def test_legacy_host_choices_unchanged(self):
        self.assertEqual(M.API_HOSTS, ("deeprouter.top", "www.deeprouter.top", "www.henapi.top", "us.deeprouter.top"))
        M.OPENER = NeverOpener()
        code, out, err = self.capture(M.main, self.gen_args("--host", "wolfai.top"))
        self.assertEqual(code, 2)
        self.assertIn("invalid choice", err)

    def test_env_pair_wins_over_file_and_legacy(self):
        self.file_config()
        os.environ["DEEPROUTER_IMAGE_BASE_URL"] = "https://env.example.com/v1/"
        os.environ["DEEPROUTER_IMAGE_API_KEY"] = IMG_ENV_KEY
        req = self.generate_ok()
        self.assertEqual(req.full_url, "https://env.example.com/v1/images/generations")
        self.assertEqual(req.get_header("Authorization"), "Bearer " + IMG_ENV_KEY)

    def test_half_set_env_is_an_error(self):
        self.file_config()
        for name, value in (("DEEPROUTER_IMAGE_BASE_URL", "https://env.example.com"),
                            ("DEEPROUTER_IMAGE_API_KEY", IMG_ENV_KEY)):
            os.environ.pop("DEEPROUTER_IMAGE_BASE_URL", None)
            os.environ.pop("DEEPROUTER_IMAGE_API_KEY", None)
            os.environ[name] = value
            err = self.assert_refused(self.gen_args(), "set both DEEPROUTER_IMAGE_BASE_URL and DEEPROUTER_IMAGE_API_KEY")
            self.assertIn("only %s is set" % name, err)

    def test_env_pair_bad_address_refused(self):
        os.environ["DEEPROUTER_IMAGE_API_KEY"] = IMG_ENV_KEY
        for base, want in (("http://env.example.com", "must start with https://"),
                           ("https://u:p@env.example.com", "username or password"),
                           ("https://env.example.com/?q=1", "query string or fragment"),
                           ("https://env.example.com/#f", "query string or fragment"),
                           ("https://", "missing a host"),
                           ("https://env.example.com:notaport", "not a valid URL"),
                           ("env.example.com", "must start with https://")):
            os.environ["DEEPROUTER_IMAGE_BASE_URL"] = base
            self.assert_refused(self.gen_args(), want)

    def test_env_pair_with_host_refused(self):
        os.environ["DEEPROUTER_IMAGE_BASE_URL"] = "https://env.example.com"
        os.environ["DEEPROUTER_IMAGE_API_KEY"] = IMG_ENV_KEY
        self.assert_refused(self.gen_args("--host", "deeprouter.top"), "--host applies only to DEEPROUTER_API_KEY")

    def test_file_pair_wins_over_legacy_key(self):
        self.file_config("https://img.example.com/api/v1")
        req = self.generate_ok()
        self.assertEqual(req.full_url, "https://img.example.com/api/v1/images/generations")
        self.assertEqual(req.get_header("Authorization"), "Bearer " + FILE_KEY)
        self.assertNotIn(FAKE_KEY, str(req.header_items()))

    def test_file_pair_for_edit(self):
        self.file_config("https://wolfai.top")
        img = os.path.join(self.tmp.name, "in.png")
        with open(img, "wb") as f:
            f.write(PNG)
        M.OPENER = FakeOpener(FakeResp(self.OK))
        code, out, err = self.capture(M.main, ["edit", "--confirm", "--prompt", "x", "--image", img,
                                               "--out-dir", os.path.join(self.tmp.name, "o")])
        self.assertIsNone(code, err)
        req = M.OPENER.requests[0]
        self.assertEqual(req.full_url, "https://wolfai.top/v1/images/edits")
        self.assertEqual(req.get_header("Authorization"), "Bearer " + FILE_KEY)

    def test_file_pair_with_host_refused(self):
        self.file_config()
        self.assert_refused(self.gen_args("--host", "deeprouter.top"), "--host applies only to DEEPROUTER_API_KEY")

    def test_incomplete_or_missing_file_falls_back_to_legacy(self):
        req = self.generate_ok()
        self.assertEqual(req.get_header("Authorization"), "Bearer " + FAKE_KEY)
        for doc in ({"base_url": "https://img.example.com"}, {"api_key": FILE_KEY}, {"base_url": "", "api_key": ""}, {}):
            self.write_config(doc)
            req = self.generate_ok()
            self.assertEqual(req.full_url, "https://deeprouter.top/v1/images/generations")
            self.assertEqual(req.get_header("Authorization"), "Bearer " + FAKE_KEY)

    def test_no_key_anywhere(self):
        os.environ.pop("DEEPROUTER_API_KEY", None)
        self.assert_refused(self.gen_args(), "no image API token")
        self.write_config({"base_url": "https://img.example.com"})
        self.assert_refused(self.gen_args(), "no image API token")

    def test_bad_config_files_refused(self):
        cases = (
            ({"base_url": "http://img.example.com", "api_key": FILE_KEY}, None, "must start with https://"),
            ({"base_url": "https://u@img.example.com", "api_key": FILE_KEY}, None, "username or password"),
            ({"base_url": "https://img.example.com?x=1", "api_key": FILE_KEY}, None, "query string"),
            ({"base_url": "https://img.example.com", "api_key": "a\nb"}, None, "single-line"),
            (None, "{not json " + FILE_KEY, "not valid JSON"),
            (None, "[1, 2]", "not a JSON object"),
            (None, " " * (M.MAX_CONFIG_BYTES + 1), "larger than"),
        )
        for doc, raw, want in cases:
            self.write_config(doc, raw)
            self.assert_refused(self.gen_args(), want)

    @unittest.skipUnless(hasattr(os, "symlink") and os.name != "nt", "needs POSIX symlinks")
    def test_symlinked_or_non_regular_config_refused(self):
        target = os.path.join(self.tmp.name, "elsewhere.json")
        with open(target, "w") as f:
            json.dump({"base_url": "https://img.example.com", "api_key": FILE_KEY}, f)
        d = os.path.join(self.home.name, ".deeprouter")
        os.makedirs(d)
        path = os.path.join(d, "image-skill.json")
        os.symlink(target, path)
        self.assert_refused(self.gen_args(), "symbolic link")
        os.remove(path)
        os.mkdir(path)
        self.assert_refused(self.gen_args(), "not a regular file")

    def test_key_in_use_is_redacted(self):
        self.file_config()
        os.environ.pop("DEEPROUTER_API_KEY", None)
        leak = json.dumps({"error": {"message": "bad key %s for %s" % (FILE_KEY, QUERY_KEY_URL)}}).encode()
        M.OPENER = FakeOpener(http_error(401, leak))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assertNotIn(FILE_KEY, out + err)
        self.assertNotIn("ResourceWarning", out + err)
        self.assert_clean(out + err)
        # A key that is not sk- shaped is still removed, because it is the key in use.
        plain = "plainFileKeyValue1234567890ZZ"
        self.file_config(key=plain)
        M.OPENER = FakeOpener(http_error(401, json.dumps({"error": {"message": "nope " + plain}}).encode()))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assertNotIn(plain, out + err)

    def test_result_download_never_gets_the_key(self):
        for setup in ("file", "env"):
            if setup == "file":
                self.file_config()
            else:
                os.environ["DEEPROUTER_IMAGE_BASE_URL"] = "https://env.example.com"
                os.environ["DEEPROUTER_IMAGE_API_KEY"] = IMG_ENV_KEY
            M.OPENER = FakeOpener(FakeResp({"data": [{"url": SIGNED_URL}]}), FakeResp(PNG))
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertIsNone(code, err)
            get = M.OPENER.requests[1]
            self.assertEqual(get.get_method(), "GET")
            for h in ("Authorization", "Cookie", "Referer"):
                self.assertIsNone(get.get_header(h))
            for k in (FILE_KEY, IMG_ENV_KEY, FAKE_KEY):
                self.assertNotIn(k, str(get.header_items()))
            self.assert_clean(out + err)

    def test_private_result_host_still_refused_with_file_pair(self):
        self.file_config()
        M.OPENER = FakeOpener(FakeResp({"data": [{"url": "https://169.254.169.254/latest"}]}))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assertEqual(len(M.OPENER.requests), 1, "no GET to a private address")
        self.assert_clean(out + err)

    def test_redirect_refused_with_file_pair_and_closed(self):
        self.file_config()
        M.OPENER = FakeOpener(own(urllib.error.HTTPError("https://img.example.com/v1/images/generations", 302,
                                                         "Found", {"Location": SIGNED_URL}, io.BytesIO(b""))))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assertIn("redirect refused", err)
        self.assertNotIn("ResourceWarning", out + err)
        self.assert_clean(out + err)

    def test_parse_image_base(self):
        good = {
            "https://deeprouter.top": "https://deeprouter.top/v1",
            "https://wolfai.top/": "https://wolfai.top/v1",
            "https://www.linkmesh.org/v1": "https://www.linkmesh.org/v1",
            "https://Img.Example.com:8443/api/v1/": "https://img.example.com:8443/api/v1",
        }
        for raw, want in good.items():
            self.assertEqual(M.parse_image_base(raw), want)
        for raw in ("", "http://x.example", "x.example", "https://a@x.example", "https://x.example/?a",
                    "https://x.example/#a", "https://", "https://x .example", "https://x.example\\@y"):
            with self.assertRaises(ValueError, msg=raw):
                M.parse_image_base(raw)

class ModelSourceTests(Base):
    """Model sent: --model, else the saved model (file pair only), else gpt-image-2.6-flare."""

    OK = ApiSourceTests.OK
    write_config = ApiSourceTests.write_config
    file_config = ApiSourceTests.file_config
    generate_ok = ApiSourceTests.generate_ok
    assert_refused = ApiSourceTests.assert_refused

    def sent_model(self, *extra):
        return json.loads(self.generate_ok(*extra).data.decode("utf-8"))["model"]

    def file_with_model(self, model):
        return self.write_config({"version": 1, "provider": "custom", "base_url": "https://img.example.com",
                                  "api_key": FILE_KEY, "model": model})

    def edit_model(self, *extra):
        img = os.path.join(self.tmp.name, "in.png")
        with open(img, "wb") as f:
            f.write(PNG)
        M.OPENER = FakeOpener(FakeResp(self.OK))
        code, out, err = self.capture(M.main, ["edit", "--confirm", "--prompt", "x", "--image", img,
                                               "--out-dir", os.path.join(self.tmp.name, "o")] + list(extra))
        self.assertIsNone(code, err)
        self.assert_clean(out + err)
        body = M.OPENER.requests[0].data
        start = body.index(b'name="model"\r\n\r\n') + len(b'name="model"\r\n\r\n')
        return body[start:body.index(b"\r\n", start)].decode("utf-8")

    def test_default_without_any_config_is_flare(self):
        self.assertEqual(M.DEFAULT_MODEL, "gpt-image-2.6-flare")
        self.assertEqual(self.sent_model(), "gpt-image-2.6-flare")
        self.assertEqual(self.edit_model(), "gpt-image-2.6-flare")

    def test_parser_has_no_model_default(self):
        a = M.build_parser().parse_args(["generate", "--prompt", "x"])
        self.assertIsNone(a.model)

    def test_saved_model_is_sent(self):
        self.file_with_model("grok-3-image")
        self.assertEqual(self.sent_model(), "grok-3-image")
        self.assertEqual(self.edit_model(), "grok-3-image")

    def test_flag_wins_over_saved_model(self):
        self.file_with_model("grok-3-image")
        self.assertEqual(self.sent_model("--model", "gpt-image-2"), "gpt-image-2")
        self.assertEqual(self.edit_model("--model", "gpt-image-1"), "gpt-image-1")

    def test_extra_json_model_still_wins(self):
        self.file_with_model("grok-3-image")
        self.assertEqual(self.sent_model("--extra-json", json.dumps({"model": "doubao-seedream-5-0-260128"})),
                         "doubao-seedream-5-0-260128")

    def test_empty_or_absent_saved_model_falls_back_to_flare(self):
        for model in ("", "   ", None):
            self.file_with_model(model)
            self.assertEqual(self.sent_model(), "gpt-image-2.6-flare")
        self.file_config()
        self.assertEqual(self.sent_model(), "gpt-image-2.6-flare")

    def test_saved_model_ignored_when_the_file_pair_is_not_used(self):
        # Env pair in use: the file's model is not taken from a file whose
        # address and key are not the ones being used.
        self.file_with_model("grok-3-image")
        os.environ["DEEPROUTER_IMAGE_BASE_URL"] = "https://env.example.com"
        os.environ["DEEPROUTER_IMAGE_API_KEY"] = IMG_ENV_KEY
        self.assertEqual(self.sent_model(), "gpt-image-2.6-flare")
        os.environ.pop("DEEPROUTER_IMAGE_BASE_URL")
        os.environ.pop("DEEPROUTER_IMAGE_API_KEY")
        # Incomplete file: legacy key, default model.
        self.write_config({"base_url": "https://img.example.com", "model": "grok-3-image"})
        req = self.generate_ok()
        self.assertEqual(req.get_header("Authorization"), "Bearer " + FAKE_KEY)
        self.assertEqual(json.loads(req.data.decode("utf-8"))["model"], "gpt-image-2.6-flare")

    def test_bad_saved_model_refused_before_sending(self):
        for model in ("a b", "a\nb", "../x", "/etc/passwd", "https://evil.example", "a\\b", "x" * 201, 5, ["m"], {"m": 1}):
            self.file_with_model(model)
            self.assert_refused(self.gen_args(), "the model is not a single model id")

    def test_check_model_matches_go_rules(self):
        for ok in ("gpt-image-2.6-flare", " grok-3-image ", "vendor/flux-kontext-pro", "\u6a21\u578b-1", "my.model_v2:latest"):
            self.assertEqual(M.check_model(ok), ok.strip())
        for bad in ("", " ", "a b", "a\tb", "a\x00b", "a\x85b", "a\\b", "/x", "a/../b", "https://x", "x" * 201, "\ud800", None, 1):
            self.assertIsNone(M.check_model(bad), repr(bad))

    def test_saved_model_does_not_leak_or_change_recover(self):
        self.file_with_model("grok-3-image")
        a = M.build_parser().parse_args(["recover", "--from", "x.json"])
        self.assertFalse(hasattr(a, "model"))


FAKE_IP = "198.18.0.7"  # what a Clash/Surge fake-IP DNS answers
PROXY = "http://127.0.0.1:7890"


class RouteHandler(urllib.request.HTTPSHandler):
    """Takes the place of the network inside a real urllib opener.

    Each request is recorded with where urllib would connect: req.host is the
    proxy when the ProxyHandler tunnels the request (and _tunnel_host the result
    host for CONNECT), else the result host itself. Outcomes are (status, body)
    or (status, body, headers) tuples, or exceptions.
    """

    def __init__(self, *outcomes):
        super().__init__()
        self.outcomes = list(outcomes)
        self.requests = []
        self.routes = []

    def https_open(self, req):
        self.requests.append(req)
        self.routes.append((req.host, req._tunnel_host))
        if not self.outcomes:
            raise UnexpectedRequest("unexpected extra request")
        o = self.outcomes.pop(0)
        if isinstance(o, BaseException):
            raise o
        headers = email.message.Message()
        for k, v in (o[2] if len(o) > 2 else {}).items():
            headers[k] = v
        body = o[1] if isinstance(o[1], bytes) else json.dumps(o[1]).encode()
        resp = urllib.response.addinfourl(io.BytesIO(body), headers, req.full_url, code=o[0])
        resp.msg = "fake"  # HTTPErrorProcessor reads it
        return resp


class FakeIPDownloadTests(Base):
    """No local DNS pre-check: a fake-IP resolver answer no longer stops the GET.

    The resolver answers FAKE_IP for every lookup. The script must not look it up
    itself; the request goes to a real urllib opener whose network is RouteHandler.
    """

    def setUp(self):
        super().setUp()
        self.dns_answer = FAKE_IP

    def routed(self, *outcomes, **env):
        """A real urllib opener (ProxyHandler from the env set here) over RouteHandler, as M.OPENER."""
        for k, v in env.items():
            os.environ[k] = v
        handler = RouteHandler(*outcomes)
        M.OPENER = urllib.request.build_opener(handler, M.NoRedirect())
        return handler

    def assert_plain_get(self, req):
        self.assertEqual(req.get_method(), "GET")
        self.assertEqual(req.full_url, SIGNED_URL)
        self.assertIn("X-Amz-Signature=", req.selector, "original signed query is sent")
        for h in ("Authorization", "Cookie", "Referer"):
            self.assertIsNone(req.get_header(h))
        self.assertNotIn(FAKE_KEY, str(req.header_items()))
        self.assertIsNone(req.data)

    def test_pre_check_code_removed(self):
        for name in ("check_resolved", "resolve_host", "explicit_proxy_for"):
            self.assertFalse(hasattr(M, name), name)

    def test_fake_ip_dns_no_proxy_get_attempted_and_saved(self):
        h = self.routed((200, {"data": [{"url": SIGNED_URL}]}), (200, PNG))
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertIsNone(code, err)
        self.assert_clean(out + err)
        self.assertNotIn("non-public", err)
        self.assertEqual(h.routes, [("deeprouter.top", None), ("bucket.s3.amazonaws.com", None)], "sent directly")
        self.assert_plain_get(h.requests[1])
        self.assertEqual(self.lookups, [], "result host not resolved by the script")
        self.assertEqual(len([n for n in os.listdir(self.tmp.name) if n.endswith(".png")]), 1)
        self.assertEqual(self.recovery_files(), [])

    def test_fake_ip_connect_failure_writes_recovery_and_does_not_resend(self):
        refused = urllib.error.URLError(ConnectionRefusedError(111, "Connection refused"))
        h = self.routed((200, {"data": [{"url": SIGNED_URL}]}), refused)
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assert_clean(out + err)
        self.assertEqual([r.get_method() for r in h.requests], ["POST", "GET"], "one POST, one GET, no retry")
        self.assertIn("download failed from bucket.s3.amazonaws.com", err)
        self.assertNotIn("non-public", err)
        self.assertIn("was not resent", err)
        self.assertIn("recover --from", err)
        recs = self.recovery_files()
        self.assertEqual(len(recs), 1)
        if os.name == "posix":
            self.assertEqual(stat.S_IMODE(os.stat(recs[0]).st_mode), 0o600)
        with open(recs[0], encoding="utf-8") as f:
            self.assertEqual([it["url"] for it in json.load(f)["items"]], [SIGNED_URL])

    def test_connect_failure_is_download_error(self):
        for exc in (urllib.error.URLError(ConnectionRefusedError(111, "refused")), socket.timeout("timed out"),
                    OSError(113, "No route to host")):
            self.routed(exc)
            with self.assertRaises(M.DownloadError) as cm:
                M.download(SIGNED_URL, 5)
            self.assertNotIsInstance(cm.exception, M.UnsafeURL)
            self.assertNotIn("X-Amz-Signature", str(cm.exception))

    def test_explicit_proxy_env_still_used_by_opener(self):
        # No special case either way: the opener routes as urllib decides.
        h = self.routed((200, PNG), HTTPS_PROXY=PROXY)
        self.assertEqual(M.download(SIGNED_URL, 5), PNG)
        self.assertEqual(h.routes, [("127.0.0.1:7890", "bucket.s3.amazonaws.com")])
        self.assert_plain_get(h.requests[0])
        h = self.routed((200, PNG), NO_PROXY=".amazonaws.com")
        self.assertEqual(M.download(SIGNED_URL, 5), PNG)
        self.assertEqual(h.routes, [("bucket.s3.amazonaws.com", None)])
        self.assertEqual(self.lookups, [])

    def test_proxy_in_effect_is_reported(self):
        cred_proxy = "http://proxyuser:hunter2pass@127.0.0.1:7890"
        for env, want in (({}, "proxy none"), ({"HTTPS_PROXY": cred_proxy}, "proxy 127.0.0.1:7890 (environment)"),
                          ({"https_proxy": PROXY, "NO_PROXY": ".amazonaws.com"}, "proxy none")):
            for k in ("HTTPS_PROXY", "https_proxy", "NO_PROXY"):
                os.environ.pop(k, None)
            h = self.routed((403, ("<html>%s</html>" % BODY_MARKER).encode(), {"Server": "nginx"}), **env)
            with self.assertRaises(M.DownloadError) as cm:
                M.download(SIGNED_URL, 5)
            msg = str(cm.exception)
            self.assertFalse(cm.exception.retryable)
            self.assertRegex(msg, r"\Adownload HTTP 403 from bucket\.s3\.amazonaws\.com \[\d+ ms; %s; "
                                  r"reason fake; Server: nginx\]\Z" % re.escape(want))
            self.assertEqual(len(h.requests), 1)
            self.assert_plain_get(h.requests[0])
            for bad in ("proxyuser", "hunter2pass", BODY_MARKER, "X-Amz-Signature"):
                self.assertNotIn(bad, msg)
        self.assertEqual(self.lookups, [])

    def test_proxy_credentials_not_in_output_or_recovery_file(self):
        h = self.routed((200, {"data": [{"url": SIGNED_URL}]}), (403, b"denied", {"Server": "nginx"}),
                        HTTPS_PROXY="http://proxyuser:hunter2pass@127.0.0.1:7890")
        code, out, err = self.capture(M.main, self.gen_args())
        self.assertEqual(code, 1)
        self.assertEqual([r.get_method() for r in h.requests], ["POST", "GET"])
        self.assertIn("proxy 127.0.0.1:7890 (environment)", err)
        self.assert_clean(out + err)
        recs = self.recovery_files()
        with open(recs[0], encoding="utf-8") as f:
            text = f.read()
        self.assertIn("proxy 127.0.0.1:7890 (environment)", json.loads(text)["items"][0]["error"])
        for bad in ("proxyuser", "hunter2pass", "Proxy-authorization", "denied"):
            self.assertNotIn(bad, out + err + text)

    def test_non_env_proxy_is_labelled_system_settings(self):
        # A ProxyHandler whose proxy did not come from the environment (as with macOS or
        # Windows system settings, which urllib reads only when no *_proxy variable is set).
        handler = RouteHandler(urllib.error.URLError(ConnectionResetError(104, "reset")),
                               urllib.error.URLError(ConnectionRefusedError(111, "refused")))
        M.OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({"https": "http://10.9.8.7:3128"}),
                                               handler, M.NoRedirect())
        with self.assertRaises(M.DownloadError) as cm:
            M.download(SIGNED_URL, 5)
        self.assertTrue(cm.exception.retryable)
        self.assertRegex(str(cm.exception), r"\[\d+ ms; proxy 10\.9\.8\.7:3128 \(system settings\)\]\Z")
        with self.assertRaises(M.DownloadError) as cm:
            M.download(SIGNED_URL, 5)
        self.assertFalse(cm.exception.retryable)
        self.assertEqual(handler.routes, [("10.9.8.7:3128", "bucket.s3.amazonaws.com")] * 2)

    def test_static_url_checks_still_apply(self):
        for env in ({}, {"HTTPS_PROXY": PROXY}):
            h = self.routed(**env)
            for url in ("https://198.18.0.7/x.png", "https://198.19.255.1/x.png", "https://10.0.0.5/x",
                        "https://127.0.0.1/x", "https://[::1]/x", "https://localhost/x", "https://printer.local/x",
                        "https://169.254.169.254/latest/meta-data", "https://metadata.google.internal/x",
                        "http://bucket.s3.amazonaws.com/x", "https://user:pw@bucket.s3.amazonaws.com/x"):
                with self.assertRaises(M.UnsafeURL, msg=url):
                    M.download(url, 5)
            self.assertEqual(h.requests, [], env)

    def test_redirect_refused(self):
        h = self.routed((302, b"", {"Location": "https://169.254.169.254/latest/meta-data"}))
        with self.assertRaises(M.DownloadError) as cm:
            M.download(SIGNED_URL, 5)
        self.assertIn("download HTTP 302", str(cm.exception))
        self.assertEqual(len(h.requests), 1, "redirect not followed")
        self.assertNotIn("169.254", str(cm.exception))

    def test_oversized_download_refused(self):
        self.routed((200, b"x" * (M.MAX_DOWNLOAD_BYTES + 1)))
        with self.assertRaises(M.DownloadError) as cm:
            M.download(SIGNED_URL, 5)
        self.assertIn("size limit", str(cm.exception))

    def write_rec(self, urls):
        path = os.path.join(self.tmp.name, "deeprouter-download-recover-test.json")
        with open(path, "w", encoding="utf-8") as f:
            json.dump({"kind": M.RECOVER_KIND, "version": 1,
                       "items": [{"index": i, "url": u, "stem": "img-%d" % i} for i, u in enumerate(urls, 1)]}, f)
        return path

    def test_recover_fake_ip_get_only(self):
        other = SIGNED_URL.replace("img.png", "img2.png")
        rec = self.write_rec([SIGNED_URL, other])
        os.environ["DEEPROUTER_IMAGE_BASE_URL"] = "https://img.example.com/v1"
        os.environ["DEEPROUTER_IMAGE_API_KEY"] = IMG_ENV_KEY
        h = self.routed((200, PNG), urllib.error.URLError(ConnectionRefusedError(111, "refused")))
        code, out, err = self.capture(M.main, ["recover", "--from", rec])
        self.assertEqual(code, 1)
        self.assert_clean(out + err)
        self.assertEqual([r.get_method() for r in h.requests], ["GET", "GET"], "no POST")
        self.assertEqual(h.routes, [("bucket.s3.amazonaws.com", None)] * 2)
        self.assert_plain_get(h.requests[0])
        for r in h.requests:
            self.assertNotIn(IMG_ENV_KEY, str(r.header_items()))
        self.assertEqual(self.lookups, [])
        self.assertTrue(os.path.exists(os.path.join(self.tmp.name, "img-1.png")))
        left = [p for p in self.recovery_files() if p != rec]
        self.assertEqual(len(left), 1)
        if os.name == "posix":
            self.assertEqual(stat.S_IMODE(os.stat(left[0]).st_mode), 0o600)
        with open(left[0], encoding="utf-8") as f:
            self.assertEqual([it["url"] for it in json.load(f)["items"]], [other])

    def test_recover_refuses_literal_private_url_without_get(self):
        for url in ("https://198.18.0.7/x.png", "https://10.0.0.5/x.png", "https://localhost/x.png"):
            rec = self.write_rec([url])
            h = self.routed()
            code, out, err = self.capture(M.main, ["recover", "--from", rec])
            self.assertEqual(code, 2, url)
            self.assertIn("recovery file rejected", err)
            self.assertEqual(h.requests, [], "no GET, no POST")


if __name__ == "__main__":
    unittest.main(verbosity=1)
