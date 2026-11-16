#!/usr/bin/env python3
"""Offline tests for deeprouter-image-api/scripts/deeprouter_image.py.

No network: OPENER.open is replaced in every test; a guard fails the test if
anything tries to reach the real opener.

Run:  python3 -I test_deeprouter_image.py
(set DEEPROUTER_IMAGE_SCRIPT to test another copy of the script)
"""

import base64
import contextlib
import gc
import importlib.util
import io
import json
import os
import socket
import ssl
import stat
import sys
import tempfile
import unittest
import urllib.error
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

FORBIDDEN = [
    FAKE_KEY, FAKE_KEY[3:], OTHER_KEY, OTHER_KEY[8:], "X-Amz-Signature", "9f8e7d6c5b4a3928",
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


class FakeOpener(object):
    """Replays a list of outcomes (FakeResp or exception) and records requests."""

    def __init__(self, *outcomes):
        self.outcomes = list(outcomes)
        self.requests = []

    def open(self, req, timeout=None):
        self.requests.append(req)
        if not self.outcomes:
            raise AssertionError("unexpected extra request (no retries allowed)")
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


class Base(unittest.TestCase):
    def setUp(self):
        self._env = os.environ.get("DEEPROUTER_API_KEY")
        os.environ["DEEPROUTER_API_KEY"] = FAKE_KEY
        self._opener = M.OPENER
        M.OPENER = NeverOpener()
        # No DNS in tests: every host "resolves" to a public documentation-free address.
        self._resolve = M.resolve_host
        self.resolved = []
        M.resolve_host = lambda host, port: self.resolved.append(host) or ["93.184.216.34"]
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        M.OPENER = self._opener
        M.resolve_host = self._resolve
        if self._env is None:
            os.environ.pop("DEEPROUTER_API_KEY", None)
        else:
            os.environ["DEEPROUTER_API_KEY"] = self._env
        while _TEST_ERRORS:
            _TEST_ERRORS.pop().close()
        self.tmp.cleanup()

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
        for exc in (urllib.error.URLError(LEAKY_TEXT), TimeoutError(LEAKY_TEXT),
                    http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL), ValueError(LEAKY_TEXT)):
            M.OPENER = FakeOpener(FakeResp({"data": [{"url": SIGNED_URL}]}), exc)
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertEqual(code, 1)
            self.assert_clean(out + err)
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

    def test_host_resolving_to_private_address_refused(self):
        for addr in ("10.0.0.5", "127.0.0.1", "169.254.169.254", "fd00:ec2::254", "::1"):
            M.resolve_host = lambda host, port, addr=addr: ["93.184.216.34", addr]
            M.OPENER = FakeOpener(FakeResp({"data": [{"url": SIGNED_URL}]}))
            code, out, err = self.capture(M.main, self.gen_args())
            self.assertEqual(code, 1)
            self.assertEqual(len(M.OPENER.requests), 1, "no GET when %s" % addr)
            self.assertIn("non-public address", err)
            self.assert_clean(out + err)


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
        self.assertEqual(doc["items"], [{"index": 1, "url": SIGNED_URL, "stem": "deeprouter-generate-1",
                                         "fallback_ext": "png", "error": "download HTTP 403 from bucket.s3.amazonaws.com"}])
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
        for exc in (http_error(403, LEAKY_TEXT.encode(), url=SIGNED_URL), TimeoutError(LEAKY_TEXT)):
            M.OPENER = FakeOpener(exc)
            code, out, err = self.recover("--from", rec)
            self.assertEqual(code, 1)
            self.assertEqual([r.get_method() for r in M.OPENER.requests], ["GET"])
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
            M.resolve_host = lambda host, port: self.fail("no DNS for a rejected file")
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

    def test_recover_resolved_private_address_no_get(self):
        rec = self.write_rec({"kind": M.RECOVER_KIND, "version": 1, "items": [{"url": SIGNED_URL}]})
        M.resolve_host = lambda host, port: ["169.254.169.254"]
        code, out, err = self.recover("--from", rec)
        self.assertEqual(code, 1)
        self.assertIn("non-public address", err)
        self.assert_clean(out + err)
        self.assertEqual(self.recovery_files(), [rec])

    def test_recover_does_not_need_confirm_and_has_no_api_options(self):
        rec = self.write_rec({"kind": M.RECOVER_KIND, "version": 1, "items": [{"url": SIGNED_URL}]})
        for extra in (["--confirm"], ["--host", "deeprouter.top"], ["--prompt", "x"]):
            code, out, err = self.recover("--from", rec, *extra)
            self.assertEqual(code, 2, extra)


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


if __name__ == "__main__":
    unittest.main(verbosity=1)
