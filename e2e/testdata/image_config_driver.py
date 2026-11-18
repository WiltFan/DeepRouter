#!/usr/bin/env python3
"""Drive an *installed* deeprouter_image.py against the image config that the
real deeprouter binary saved in a temporary HOME.

Run by e2e/skill_accept_test.go:

    python3 -I -B image_config_driver.py --script INSTALLED.py --work DIR --out RESULT.json --expect-base URL --expect-model ID

Environment: HOME is the temporary home holding ~/.deeprouter/image-skill.json.
DEEPROUTER_API_KEY is a different fake key that must NOT be used while the file
pair is in force. --expect-model is the model the binary saved; it must be
sent unless --model overrides it. DEEPROUTER_E2E_EXPECT_KEY is the fake key typed into the
binary; it is only compared, never printed or written.

Mock boundary: OPENER (HTTP) is replaced, and sockets are made to fail. Nothing reaches the network. The
result JSON never contains a key: requests are described as URL, method and
which fake key (by name) was in the Authorization header.
"""

import argparse
import base64
import contextlib
import importlib.util
import io
import json
import os
import socket
import sys
import traceback
import urllib.error

EXPECT_KEY = os.environ.get("DEEPROUTER_E2E_EXPECT_KEY", "")
LEGACY_KEY = os.environ.get("DEEPROUTER_API_KEY", "")
ENV_KEY = "sk-test-e2eImageEnvPairKEY000000000000"
PNG = b"\x89PNG\r\n\x1a\n" + b"e2e-image-config" + bytes(range(64))
RESULT_URL = "https://results.e2e-cdn.example/out/x.png?X-Amz-Signature=e2eimgcfgsig0000"


def block_network():
    def refuse(*a, **k):
        raise RuntimeError("real network access attempted from the e2e driver")
    socket.socket.connect = refuse
    socket.create_connection = refuse
    socket.getaddrinfo = refuse


class FakeResp(object):
    def __init__(self, body):
        self._body = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.status = 200

    def read(self, n=-1):
        return self._body if n is None or n < 0 else self._body[:n]

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False


class FakeOpener(object):
    def __init__(self, *outcomes):
        self.outcomes = list(outcomes)
        self.requests = []

    def open(self, req, timeout=None):
        self.requests.append(req)
        if not self.outcomes:
            raise AssertionError("unexpected extra request")
        o = self.outcomes.pop(0)
        if isinstance(o, BaseException):
            raise o
        return o


def which_key(value):
    if value is None:
        return None
    for name, key in (("expected", EXPECT_KEY), ("legacy DEEPROUTER_API_KEY", LEGACY_KEY), ("env pair", ENV_KEY)):
        if key and value == "Bearer " + key:
            return name
    return "other"


def sent_model(req):
    """The model in a generate (JSON) body, or None."""
    if req.get_method() != "POST" or not req.data:
        return None
    try:
        return json.loads(req.data.decode("utf-8")).get("model")
    except ValueError:
        return None


def describe(req):
    h = {k.lower(): v for k, v in req.header_items()}
    base, _, q = req.full_url.partition("?")
    return {"method": req.get_method(), "url": base + ("?<query>" if q else ""),
            "authorization": which_key(h.get("authorization")),
            "cookie": "cookie" in h, "referer": "referer" in h, "model": sent_model(req)}


def leaks(text):
    return [n for n, k in (("expected", EXPECT_KEY), ("legacy", LEGACY_KEY), ("env", ENV_KEY)) if k and k in text]


def run_main(M, argv, opener):
    M.OPENER = opener
    out, err = io.StringIO(), io.StringIO()
    code = None
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        try:
            M.main(argv)
        except SystemExit as e:
            code = e.code
    return code or 0, out.getvalue(), err.getvalue()


def case(name, M, argv, opener, checks_fn, env=None):
    saved = {k: os.environ.get(k) for k in ("DEEPROUTER_IMAGE_BASE_URL", "DEEPROUTER_IMAGE_API_KEY")}
    for k, v in (env or {}).items():
        os.environ[k] = v
    try:
        code, out, err = run_main(M, argv, opener)
    finally:
        for k, v in saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
    reqs = [describe(r) for r in opener.requests]
    checks = [{"name": "no key in stdout/stderr", "ok": not leaks(out + err), "detail": ", ".join(leaks(out + err))}]
    checks += checks_fn(code, reqs, out, err)
    return {"name": name, "argv": argv, "exit": code, "stdout": out[-600:], "stderr": err[-600:],
            "requests": reqs, "checks": checks}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--script", required=True)
    ap.add_argument("--work", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--expect-base", required=True)
    ap.add_argument("--expect-model", required=True)
    a = ap.parse_args()
    block_network()
    spec = importlib.util.spec_from_file_location("installed_deeprouter_image", a.script)
    M = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(M)
    expect = a.expect_base.rstrip("/") + "/v1/images/generations"
    gen = ["generate", "--confirm", "--prompt", "e2e", "--out-dir", a.work]
    ok = {"data": [{"b64_json": base64.b64encode(PNG).decode()}]}
    cases = []
    try:
        cases.append(case("file_pair_used", M, gen, FakeOpener(FakeResp(ok)), lambda code, reqs, out, err: [
            {"name": "exit 0", "ok": code == 0, "detail": err[-200:]},
            {"name": "one POST to the saved address", "ok": len(reqs) == 1 and reqs[0]["url"] == expect, "detail": json.dumps(reqs)},
            {"name": "Authorization is the saved key, not DEEPROUTER_API_KEY", "ok": bool(reqs) and reqs[0]["authorization"] == "expected", "detail": json.dumps(reqs)},
            {"name": "body model is the saved model", "ok": bool(reqs) and reqs[0]["model"] == a.expect_model, "detail": json.dumps(reqs)},
        ]))
        cases.append(case("model_flag_wins", M, gen + ["--model", "gpt-image-1"], FakeOpener(FakeResp(ok)),
                          lambda code, reqs, out, err: [
            {"name": "exit 0", "ok": code == 0, "detail": err[-200:]},
            {"name": "body model is --model", "ok": len(reqs) == 1 and reqs[0]["model"] == "gpt-image-1", "detail": json.dumps(reqs)},
        ]))
        cases.append(case("file_pair_download_has_no_key", M, gen,
                          FakeOpener(FakeResp({"data": [{"url": RESULT_URL}]}), FakeResp(PNG)),
                          lambda code, reqs, out, err: [
            {"name": "exit 0", "ok": code == 0, "detail": err[-200:]},
            {"name": "result GET has no Authorization, Cookie, Referer", "ok": len(reqs) == 2 and reqs[1]["method"] == "GET"
             and reqs[1]["authorization"] is None and not reqs[1]["cookie"] and not reqs[1]["referer"], "detail": json.dumps(reqs)},
        ]))
        cases.append(case("env_pair_overrides_file", M, gen, FakeOpener(FakeResp(ok)), lambda code, reqs, out, err: [
            {"name": "exit 0", "ok": code == 0, "detail": err[-200:]},
            {"name": "POST to the env address with the env key", "ok": len(reqs) == 1
             and reqs[0]["url"] == "https://env.e2e.example/v1/images/generations" and reqs[0]["authorization"] == "env pair",
             "detail": json.dumps(reqs)},
            {"name": "file model not used with the env pair: default gpt-image-2.6-flare", "ok": len(reqs) == 1
             and reqs[0]["model"] == "gpt-image-2.6-flare", "detail": json.dumps(reqs)},
        ], env={"DEEPROUTER_IMAGE_BASE_URL": "https://env.e2e.example/v1", "DEEPROUTER_IMAGE_API_KEY": ENV_KEY}))
        cases.append(case("half_env_pair_refused", M, gen, FakeOpener(), lambda code, reqs, out, err: [
            {"name": "exit 2", "ok": code == 2, "detail": err[-200:]},
            {"name": "no request", "ok": not reqs, "detail": json.dumps(reqs)},
        ], env={"DEEPROUTER_IMAGE_BASE_URL": "https://env.e2e.example"}))
        cases.append(case("host_flag_refused_with_file", M, gen + ["--host", "deeprouter.top"], FakeOpener(),
                          lambda code, reqs, out, err: [
            {"name": "exit 2", "ok": code == 2, "detail": err[-200:]},
            {"name": "no request", "ok": not reqs, "detail": json.dumps(reqs)},
        ]))
        rec = os.path.join(a.work, "recover-e2e.json")
        with open(rec, "w") as f:
            json.dump({"kind": M.RECOVER_KIND, "version": M.RECOVER_VERSION, "items": [{"url": RESULT_URL}]}, f)
        real = (M.config_path, M.read_image_config, M.resolve_api)

        def boom(*_a, **_k):
            raise AssertionError("recover touched the API config")
        M.config_path = M.read_image_config = M.resolve_api = boom
        try:
            cases.append(case("recover_reads_no_config", M, ["recover", "--from", rec], FakeOpener(FakeResp(PNG)),
                              lambda code, reqs, out, err: [
                {"name": "exit 0", "ok": code == 0, "detail": err[-200:]},
                {"name": "one GET, no Authorization", "ok": len(reqs) == 1 and reqs[0]["method"] == "GET"
                 and reqs[0]["authorization"] is None, "detail": json.dumps(reqs)},
            ]))
        finally:
            M.config_path, M.read_image_config, M.resolve_api = real
    except Exception:
        cases.append({"name": "driver_crash", "exit": -1, "stdout": "", "stderr": traceback.format_exc()[-1500:],
                      "requests": [], "checks": [{"name": "driver did not crash", "ok": False, "detail": "see stderr"}]})
    text = json.dumps({"cases": cases}, indent=2)
    if leaks(text):
        text = json.dumps({"cases": [{"name": "driver_leak", "exit": -1, "stdout": "", "stderr": "", "requests": [],
                                      "checks": [{"name": "driver output holds no key", "ok": False, "detail": ""}]}]})
    with open(a.out, "w") as f:
        f.write(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
