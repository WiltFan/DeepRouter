#!/usr/bin/env python3
"""Drive an *installed* copy of deeprouter_image.py through generate -> 403 -> recover.

Run by e2e/skill_accept_test.go:

    python3 -I -B skill_script_driver.py --script INSTALLED.py --work DIR --out RESULT.json

Mock boundary: only OPENER (HTTP) is replaced. check_result_url, redact,
NoRedirect and the "no Authorization on result GET" code run unchanged; the
script does no DNS pre-check of its own. socket connect and
getaddrinfo are also made to fail, so a request that slipped past the fake
opener cannot reach the network. Nothing here proves a live 403 from a real
result host.

The result JSON never contains the key or the result URL query: URLs are
logged as scheme://host/path plus whether a query was sent.
"""

import argparse
import base64
import contextlib
import glob
import importlib.util
import io
import json
import os
import socket
import stat
import sys
import traceback
import urllib.error

FAKE_KEY = os.environ.get("DEEPROUTER_API_KEY", "")
SIG = "e2e5ign4ture0f8e7d6c5b4a39281706f5e4d3c2b1a0fedcba98765432"
CRED = "AKIAE2EEXAMPLE%2F20261116%2Fus-east-1%2Fs3%2Faws4_request"
HOST = "results.e2e-cdn.example"


def signed(name):
    return ("https://%s/out/20261116/%s.png?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=%s"
            "&X-Amz-Date=20261116T120000Z&X-Amz-Expires=3600&X-Amz-SignedHeaders=host"
            "&X-Amz-Signature=%s" % (HOST, name, CRED, SIG))


URL1 = signed("a1")
URL2 = signed("b2")
PNG = b"\x89PNG\r\n\x1a\n" + b"IHDR-e2e-payload-" + bytes(range(256)) * 4
GIF = b"GIF89a" + b"e2e-gif-payload" + bytes(range(200))
B64_PNG = base64.b64encode(b"\x89PNG\r\n\x1a\n" + os.urandom(3000)).decode()
LEAKY_403 = ("<Error><Code>AccessDenied</Code><Message>Request has expired</Message>"
             "<X-Amz-Signature>%s</X-Amz-Signature><Bearer>%s</Bearer></Error>" % (SIG, FAKE_KEY)).encode()
API = "https://deeprouter.top/v1/images/generations"

# Strings that must never be printed by the script or written by this driver.
FORBIDDEN = [s for s in (FAKE_KEY, FAKE_KEY[3:], SIG, SIG[:16], "X-Amz-Signature", "X-Amz-Credential",
                         "AKIAE2EEXAMPLE", "Bearer ", B64_PNG[:40]) if s]


def safe(url):
    base, _, q = url.partition("?")
    return base + ("?<query %d bytes>" % len(q) if q else "")


class Net(Exception):
    pass


def block_network():
    def refuse(*a, **k):
        raise Net("real network access attempted from the e2e driver")
    socket.socket.connect = refuse
    socket.create_connection = refuse
    socket.getaddrinfo = refuse


class FakeResp(object):
    def __init__(self, body, status=200):
        self._body = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.status = status
        self.headers = {}

    def read(self, n=-1):
        return self._body if n is None or n < 0 else self._body[:n]

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class FakeOpener(object):
    """Replays outcomes in order and records every request. Extra requests fail."""

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


def http_403(url):
    return urllib.error.HTTPError(url, 403, "Forbidden " + LEAKY_403.decode(), {"Content-Type": "application/xml"},
                                  io.BytesIO(LEAKY_403))


def headers_of(req):
    h = {k.lower(): v for k, v in req.header_items()}
    return h


def describe(req):
    h = headers_of(req)
    return {
        "method": req.get_method(),
        "url": safe(req.full_url),
        "query_sent": "?" in req.selector and len(req.selector.split("?", 1)[1]) > 0,
        "header_names": sorted(h),
        "authorization_present": "authorization" in h,
    }


class Case(object):
    def __init__(self, name, argv):
        self.name = name
        self.argv = argv
        self.checks = []
        self.info = {}
        self.exit = None
        self.stdout = ""
        self.stderr = ""
        self.requests = []

    def check(self, what, ok, detail=""):
        self.checks.append({"name": what, "ok": bool(ok), "detail": str(detail)[:300]})
        return ok

    def no_leak(self, label, text):
        hits = [f[:6] + "..." for f in FORBIDDEN if f in text]
        self.check("%s has no key/signature/credential/Bearer/b64" % label, not hits, ",".join(hits))

    def result(self):
        return {"name": self.name, "argv": self.argv, "exit": self.exit, "stdout": self.stdout,
                "stderr": self.stderr, "requests": self.requests, "info": self.info, "checks": self.checks,
                "pass": all(c["ok"] for c in self.checks) and bool(self.checks)}


def run_main(M, case, opener):
    M.OPENER = opener
    out, err = io.StringIO(), io.StringIO()
    code = 0
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        try:
            M.main(case.argv)
        except SystemExit as e:
            code = e.code if isinstance(e.code, int) else (0 if e.code is None else 1)
    case.exit = code
    case.stdout, case.stderr = out.getvalue(), err.getvalue()
    for url in (URL1, URL2):
        if url in case.stdout:
            case.check("stdout returns the complete result URL", any(line.endswith(" URL: " + url) for line in case.stdout.splitlines()))
            case.stdout = case.stdout.replace(url, safe(url))
    case.requests = [describe(r) for r in opener.requests]
    case.no_leak("stdout", case.stdout)
    case.no_leak("stderr", case.stderr)


def recovery_files(d):
    return sorted(glob.glob(os.path.join(d, "deeprouter-download-recover-*.json")))


def mode(p):
    try:
        return stat.S_IMODE(os.stat(p).st_mode)
    except OSError:
        return None


def oct_(m):
    return "missing" if m is None else oct(m)


def read(p):
    try:
        with open(p, "rb") as f:
            return f.read()
    except OSError:
        return None


def check_get(case, req, url):
    h = headers_of(req)
    case.check("GET method", req.get_method() == "GET", req.get_method())
    case.check("GET is the complete signed URL", req.full_url == url, safe(req.full_url))
    case.check("GET sends the signed query", req.selector.endswith("X-Amz-Signature=" + SIG), "selector query present")
    for name in ("authorization", "cookie", "referer", "proxy-authorization"):
        case.check("GET has no %s header" % name, name not in h)
    case.check("GET uses the product User-Agent", h.get("user-agent") == "deeprouter-image-generate/1.0.10")
    case.check("GET has no body", req.data is None)


def case_generate_403(M, work):
    parent = os.path.join(work, "existing")
    os.mkdir(parent)
    os.chmod(parent, 0o755)
    out_dir = os.path.join(parent, "new1", "new2")
    argv = ["generate", "--confirm", "--prompt", "e2e test cat", "--out-dir", out_dir, "--timeout", "30"]
    c = Case("generate_then_download_403", argv)
    opener = FakeOpener(FakeResp({"created": 1763294400, "data": [{"url": URL1}]}), http_403(URL1))
    run_main(M, c, opener)
    reqs = opener.requests
    c.check("exit 3: generate succeeded, download failed", c.exit == 3, c.exit)
    c.check("exactly POST then GET (no second POST, no retry)", [r.get_method() for r in reqs] == ["POST", "GET"],
            [r.get_method() for r in reqs])
    if reqs:
        p = reqs[0]
        c.check("POST to official generations endpoint", p.full_url == API, safe(p.full_url))
        c.check("POST carries the fake key as Bearer (mock API only)",
                headers_of(p).get("authorization") == "Bearer " + FAKE_KEY)
        body = json.loads(p.data.decode())
        c.check("POST body has the prompt and n=1", body.get("prompt") == "e2e test cat" and body.get("n") == 1)
    if len(reqs) > 1:
        check_get(c, reqs[1], URL1)
    recs = recovery_files(out_dir)
    c.check("one recovery file in out-dir", len(recs) == 1, [os.path.basename(r) for r in recs])
    rec = recs[0] if recs else None
    if rec:
        c.info["recovery_file"] = rec
        c.check("recovery file mode 0600", mode(rec) == 0o600, oct_(mode(rec)))
        raw = open(rec, "rb").read()
        text = raw.decode()
        doc = json.loads(text)
        c.check("recovery file keeps the complete signed URL",
                [it.get("url") for it in doc.get("items", [])] == [URL1])
        for bad in (FAKE_KEY, "Authorization", "Bearer", "AccessDenied", "Request has expired"):
            c.check("recovery file has no %s" % (bad[:6] + "..." if bad == FAKE_KEY else bad), bad not in text)
        c.info["recovery_items"] = [{"index": it.get("index"), "url": safe(it.get("url", "")),
                                     "stem": it.get("stem"), "error": it.get("error")} for it in doc["items"]]
        c.check("stdout prints the recovery file absolute path",
                ("recovery file: " + os.path.abspath(rec)) in c.stdout)
    c.check("stderr explains manual browser download", "Open the result link in a browser and download it yourself" in c.stderr)
    c.check("stderr reports HTTP 403 and host only", "download HTTP 403 from " + HOST in c.stderr)
    c.check("stdout has the result URL (redacted in this report)", "image 1 URL: " + safe(URL1) in c.stdout)
    c.check("stderr does not echo the leaky 403 body", "AccessDenied" not in c.stderr and "expired" not in c.stderr)
    c.check("created dirs are 0700", mode(os.path.join(parent, "new1")) == 0o700 and mode(out_dir) == 0o700,
            "%s %s" % (oct_(mode(os.path.join(parent, "new1"))), oct_(mode(out_dir))))
    c.check("pre-existing parent mode not changed (0755)", mode(parent) == 0o755, oct_(mode(parent)))
    c.check("no image saved", not glob.glob(os.path.join(out_dir, "deeprouter-generate-*")))
    return c, rec


def case_recover_ok(M, work, rec):
    out_dir = os.path.join(work, "recovered")
    os.mkdir(out_dir)
    os.chmod(out_dir, 0o750)
    squatter = os.path.join(out_dir, "deeprouter-generate-1.png")
    with open(squatter, "wb") as f:
        f.write(b"pre-existing user file, must not be overwritten")
    before_sq = open(squatter, "rb").read()
    st0 = os.stat(rec)
    raw0 = open(rec, "rb").read()
    argv = ["recover", "--from", rec, "--out-dir", out_dir]
    c = Case("recover_success", argv)
    opener = FakeOpener(FakeResp(PNG))
    run_main(M, c, opener)
    reqs = opener.requests
    c.check("exit 0", c.exit == 0, c.exit)
    c.check("zero POST", not [r for r in reqs if r.get_method() == "POST"])
    c.check("exactly one GET", [r.get_method() for r in reqs] == ["GET"], [r.get_method() for r in reqs])
    if reqs:
        check_get(c, reqs[0], URL1)
    new = os.path.join(out_dir, "deeprouter-generate-1-2.png")
    c.check("saved under a unique name (-2)", os.path.isfile(new), sorted(os.listdir(out_dir)))
    if os.path.isfile(new):
        c.check("saved bytes complete (magic + payload)", open(new, "rb").read() == PNG)
        c.check("stdout prints saved path", ("saved " + new) in c.stdout)
    c.check("same-stem file not overwritten", open(squatter, "rb").read() == before_sq)
    c.check("recovery file still present, same bytes", read(rec) == raw0)
    st1 = os.stat(rec) if os.path.exists(rec) else None
    c.check("recovery file same inode/mtime/mode", st1 is not None and
            (st0.st_ino, st0.st_mtime_ns, st0.st_mode) == (st1.st_ino, st1.st_mtime_ns, st1.st_mode))
    c.check("stdout notes the recovery file was kept", "was not deleted" in c.stdout)
    c.check("out-dir mode not changed", mode(out_dir) == 0o750, oct_(mode(out_dir)))
    c.check("no new recovery file", recovery_files(out_dir) == [])
    return c


def case_recover_403_again(M, work, rec):
    out_dir = os.path.join(work, "recover-again")
    os.mkdir(out_dir)
    raw0 = read(rec)
    c = Case("recover_403_again", ["recover", "--from", rec, "--out-dir", out_dir])
    opener = FakeOpener(http_403(URL1))
    run_main(M, c, opener)
    c.check("exit non-zero", c.exit not in (0, None), c.exit)
    c.check("one GET, no POST", [r.get_method() for r in opener.requests] == ["GET"])
    if opener.requests:
        check_get(c, opener.requests[0], URL1)
    c.check("recovery file exists before this case", raw0 is not None)
    c.check("original recovery file unchanged", raw0 is not None and read(rec) == raw0)
    c.check("stderr says the original file was left unchanged", "left unchanged" in c.stderr)
    c.check("no image and no new recovery file", os.listdir(out_dir) == [], os.listdir(out_dir))
    return c


def case_b64_then_url_403(M, work):
    out_dir = os.path.join(work, "mixed")
    argv = ["generate", "--confirm", "--download", "--prompt", "two images", "--n", "2", "--out-dir", out_dir]
    c = Case("generate_b64_saved_url_403", argv)
    opener = FakeOpener(FakeResp({"data": [{"b64_json": B64_PNG}, {"url": URL2}]}), http_403(URL2))
    run_main(M, c, opener)
    c.check("exit 3: generate succeeded, download failed", c.exit == 3, c.exit)
    c.check("POST then GET only", [r.get_method() for r in opener.requests] == ["POST", "GET"])
    if len(opener.requests) > 1:
        check_get(c, opener.requests[1], URL2)
    first = os.path.join(out_dir, "deeprouter-generate-1.png")
    c.check("b64 image saved and kept", os.path.isfile(first)
            and open(first, "rb").read() == base64.b64decode(B64_PNG))
    c.check("stdout prints the saved b64 image", ("saved " + first) in c.stdout)
    recs = recovery_files(out_dir)
    c.check("one recovery file", len(recs) == 1)
    if recs:
        text = open(recs[0]).read()
        doc = json.loads(text)
        c.check("recovery has only the failed URL (index 2)",
                [(it["index"], it["url"]) for it in doc["items"]] == [(2, URL2)])
        c.check("recovery has no b64 data", B64_PNG[:40] not in text and "b64" not in text)
        c.check("recovery file has no key", FAKE_KEY not in text)
        c.check("recovery file mode 0600", mode(recs[0]) == 0o600)
    return c


def case_safety_on(M, work):
    """Controls: the real URL checks still refuse unsafe targets with zero requests."""
    c = Case("safety_controls", ["(in-process download() and recover controls)"])
    opener = FakeOpener()
    M.OPENER = opener
    for bad in ("http://%s/x.png?sig=1" % HOST, "https://127.0.0.1/x.png", "https://user@%s/x.png" % HOST,
                "https://metadata.google.internal/x"):
        try:
            M.download(bad, 5)
            c.check("refused %s" % safe(bad), False, "downloaded")
        except M.UnsafeURL:
            c.check("refused %s" % safe(bad), True)
    c.check("no local DNS pre-check left in the script (removed by request)",
            not hasattr(M, "check_resolved") and not hasattr(M, "resolve_host"))
    c.check("no request sent by any control", opener.requests == [])
    c.check("NoRedirect handler still installed in the module",
            issubclass(M.NoRedirect, M.urllib.request.HTTPRedirectHandler))
    c.exit = 0
    return c


def crashed(name):
    c = Case(name, [])
    # The traceback names only test paths and exception types.
    c.check("driver ran without crashing", False, traceback.format_exc()[-300:])
    return c


def guarded(name, fn):
    try:
        return fn()
    except BaseException:
        return crashed(name)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--script", required=True)
    ap.add_argument("--work", required=True)
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    block_network()
    cases = []
    try:
        if not FAKE_KEY.startswith("sk-test-"):
            raise SystemExit("refusing to run without a fake sk-test- key")
        spec = importlib.util.spec_from_file_location("installed_deeprouter_image", a.script)
        M = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(M)
        load = Case("load_installed_module", ["import " + a.script])
        load.exit = 0
        load.check("module loaded from the installed path", os.path.samefile(M.__file__, a.script), M.__file__)
        load.check("safety functions are the module's own",
                   all(getattr(M, f).__module__ == M.__name__ for f in
                       ("check_result_url", "redact", "download", "safe_url")))
        cases.append(load)
        gen = guarded("generate_then_download_403", lambda: case_generate_403(M, a.work))
        rec = None
        if isinstance(gen, tuple):
            gen, rec = gen
        cases.append(gen)
        if rec:
            cases.append(guarded("recover_success", lambda: case_recover_ok(M, a.work, rec)))
            cases.append(guarded("recover_403_again", lambda: case_recover_403_again(M, a.work, rec)))
        else:
            for name in ("recover_success", "recover_403_again"):
                c = Case(name, [])
                c.check("a recovery file from generate to recover from", False, "generate wrote none")
                cases.append(c)
        cases.append(guarded("generate_b64_saved_url_403", lambda: case_b64_then_url_403(M, a.work)))
        cases.append(guarded("safety_controls", lambda: case_safety_on(M, a.work)))
    except BaseException:
        cases.append(crashed("driver_crash"))
    with open(a.out, "w") as f:
        json.dump({"cases": [c.result() for c in cases]}, f, indent=2)


if __name__ == "__main__":
    main()
