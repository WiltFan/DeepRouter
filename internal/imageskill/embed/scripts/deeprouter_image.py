#!/usr/bin/env python3
"""Send ONE DeepRouter image generate/edit request and save the result locally.

- API address and key, first match wins, never mixed:
  1. DEEPROUTER_IMAGE_BASE_URL + DEEPROUTER_IMAGE_API_KEY, both set (only one
     set is an error);
  2. the image Skill config ~/.deeprouter/image-skill.json written by
     `deeprouter` (Image Skill in its program list), when it has both an address and a key;
  3. DEEPROUTER_API_KEY with an official DeepRouter host (--host, default
     https://deeprouter.top).
  The key is never taken from a command-line flag. The address must be HTTPS.
- Model sent, first match wins: --model; else the "model" saved in that config
  file, only when its address and key are the ones in use; else
  gpt-image-2.6-flare. Saved or default, a model is not checked before sending.
- Never retries, never follows redirects, never sends the key to result URLs.
- Refuses to send unless --confirm is given (a sent request may be billed).
- Never prints Base64 payloads, raw response bodies, the key, or URL queries;
  every error message is redacted and truncated before it is printed.
- Downloads result URLs only over HTTPS from public addresses. If a download
  fails after a successful request, the complete result URLs are written to a
  private local recovery file (0600) and `recover --from FILE` retries only
  the download; it never sends a generate/edit request.
- A direct download first resolves the result host and refuses any non-public
  address. A local fake-IP DNS (e.g. 198.18.x.x from Clash/Surge TUN or a system
  proxy) is refused too; the generate/edit request is not retried and
  `recover --from FILE` can retry only the download. The DNS check is skipped
  only when the download will actually go through an explicit HTTP proxy from
  this process's environment (https_proxy / HTTPS_PROXY = http://host:port,
  sent via HTTP CONNECT) and NO_PROXY / no_proxy does not bypass the host; the
  proxy then resolves the host, which means trusting that proxy's egress.
  System proxy settings, TUN, ALL_PROXY and HTTP_PROXY never skip the check.

Standard library only (Python 3.8+).
"""

import argparse
import base64
import binascii
import ipaddress
import json
import os
import re
import socket
import ssl
import stat
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

# Official API hosts listed in the DeepRouter docs. No automatic switching.
API_HOSTS = ("deeprouter.top", "www.deeprouter.top", "www.henapi.top", "us.deeprouter.top")
DEFAULT_HOST = "deeprouter.top"

# Where the API address and key come from (see resolve_api).
ENV_IMAGE_BASE = "DEEPROUTER_IMAGE_BASE_URL"
ENV_IMAGE_KEY = "DEEPROUTER_IMAGE_API_KEY"
ENV_LEGACY_KEY = "DEEPROUTER_API_KEY"
CONFIG_DIR = ".deeprouter"
CONFIG_FILE = "image-skill.json"
CONFIG_SHOWN = "~/.deeprouter/image-skill.json"  # how messages name it
MAX_CONFIG_BYTES = 64 * 1024
MAX_KEY_LEN = 4096
MAX_MODEL_LEN = 200
DEFAULT_MODEL = "gpt-image-2.6-flare"
# Keys in use for this run; redact() removes them from every message.
_ACTIVE_KEYS = []
MAX_DOWNLOAD_BYTES = 200 * 1024 * 1024
MAX_MSG = 240

# Recovery file written when a result download fails after a successful request.
RECOVER_KIND = "deeprouter-image-download-recovery"
RECOVER_VERSION = 1
RECOVER_PREFIX = "deeprouter-download-recover-"
RECOVER_NOTE = ("Contains complete signed result URLs. Private: do not share, paste, or commit it. "
                "Retry with: deeprouter_image.py recover --from THIS_FILE. Delete it when done.")
MAX_RECOVER_BYTES = 1024 * 1024
MAX_RECOVER_ITEMS = 100
MAX_URL_LEN = 16384
_STEM_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_\-]{0,99}\Z")

# Host names that never belong to a public result host.
BLOCKED_HOSTS = {"localhost", "metadata", "metadata.google.internal", "metadata.goog", "instance-data"}
BLOCKED_SUFFIXES = (".localhost", ".local", ".internal", ".localdomain", ".home.arpa")

MAGIC = (
    (b"\x89PNG\r\n\x1a\n", "png"),
    (b"\xff\xd8\xff", "jpg"),
    (b"GIF87a", "gif"),
    (b"GIF89a", "gif"),
)

# The only extensions ever used in output file names.
SAFE_EXT = {"png": "png", "jpg": "jpg", "jpeg": "jpg", "webp": "webp", "gif": "gif", "svg": "svg"}

# --extra-json keys that look like credentials (compared lower-case, '-' treated as '_').
CREDENTIAL_KEYS = {
    "authorization", "proxy_authorization", "bearer", "api_key", "apikey", "x_api_key", "key",
    "token", "access_token", "refresh_token", "id_token", "auth_token", "secret", "client_secret",
    "password", "deeprouter_api_key",
}
CREDENTIAL_PARTS = ("authorization", "api_key", "apikey", "secret", "password", "access_token", "bearer")

_URL_RE = re.compile(r"(?i)\b[a-z][a-z0-9+.\-]*://[^\s\"'<>\\]+")
_AUTH_HEADER_RE = re.compile(r"(?i)\b((?:proxy-)?authorization)([\"']?\s*[:=]\s*[\"']?)(?:bearer\s+|basic\s+)?[^\s\"',;}]+")
_BEARER_RE = re.compile(r"(?i)\bbearer\s+[A-Za-z0-9._~+/=\-]+")
_SK_RE = re.compile(r"(?i)\bsk-[A-Za-z0-9_\-]{4,}")
_ASSIGN_RE = re.compile(
    r"(?i)\b(deeprouter_api_key|x-api-key|api[_\-]?key|access[_\-]?token|refresh[_\-]?token|"
    r"client[_\-]?secret|secret|token|password|key)\b([\"']?\s*[:=]\s*[\"']?)[^\s\"'&,;}]+"
)
_LONG_DATA_RE = re.compile(r"[A-Za-z0-9+/_\-]{64,}={0,2}")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise urllib.error.HTTPError(safe_url(req.full_url), code, "redirect refused", headers, fp)


OPENER = urllib.request.build_opener(
    urllib.request.HTTPSHandler(context=ssl.create_default_context()), NoRedirect()
)


def safe_url(url):
    """scheme://host/path only: no userinfo, port is kept, query and fragment dropped."""
    try:
        p = urllib.parse.urlsplit(str(url))
        host = p.hostname or ""
        if p.port:
            host += ":%d" % p.port
        return "%s://%s%s" % (p.scheme, host, p.path)
    except ValueError:
        return "[url]"


def redact(text, limit=MAX_MSG):
    """Remove keys, auth headers, URL queries and long Base64-like runs, then truncate."""
    s = str(text)
    keys = [os.environ.get(ENV_LEGACY_KEY, ""), os.environ.get(ENV_IMAGE_KEY, "")] + _ACTIVE_KEYS
    for key in sorted({k.strip() for k in keys}, key=len, reverse=True):
        if len(key) >= 4:
            s = s.replace(key, "[REDACTED]")
    s = _URL_RE.sub(lambda m: safe_url(m.group(0)), s)
    s = _AUTH_HEADER_RE.sub(r"\1\2[REDACTED]", s)
    s = _BEARER_RE.sub("Bearer [REDACTED]", s)
    s = _SK_RE.sub("sk-[REDACTED]", s)
    s = _ASSIGN_RE.sub(r"\1\2[REDACTED]", s)
    s = _LONG_DATA_RE.sub("[long data omitted]", s)
    s = " ".join(s.split())
    if len(s) > limit:
        s = s[:limit] + "..."
    return s


def die(msg, code=1):
    # Last line of defence: every message goes through redact() again.
    print("error: " + redact(msg, limit=MAX_MSG * 2), file=sys.stderr)
    sys.exit(code)


def safe_ext(value, default="png"):
    if isinstance(value, str):
        return SAFE_EXT.get(value.strip().lower(), default)
    return default


def sniff_ext(data, fallback):
    for magic, ext in MAGIC:
        if data.startswith(magic):
            return ext
    if data[:4] == b"RIFF" and data[8:12] == b"WEBP":
        return "webp"
    head = data[:512].lstrip().lower()
    if head.startswith(b"<svg") or (head.startswith(b"<?xml") and b"<svg" in data[:4096].lower()):
        return "svg"
    return safe_ext(fallback)


def unique_path(out_dir, stem, ext):
    path = os.path.join(out_dir, "%s.%s" % (stem, ext))
    i = 2
    while os.path.exists(path):
        path = os.path.join(out_dir, "%s-%d.%s" % (stem, i, ext))
        i += 1
    return path


def write_new(out_dir, stem, data, fallback_ext):
    path = unique_path(out_dir, stem, sniff_ext(data, fallback_ext))
    if os.path.dirname(os.path.abspath(path)) != os.path.abspath(out_dir):
        die("refusing to write outside the output directory")
    with open(path, "xb") as f:
        f.write(data)
    return path


def error_summary(body):
    """Redacted code/type/message from a parsed JSON body; never the body itself."""
    err = body.get("error") if isinstance(body, dict) else None
    if isinstance(err, dict):
        parts = []
        for k in ("code", "type", "message"):
            v = err.get(k)
            if isinstance(v, (str, int, float)) and not isinstance(v, bool) and str(v).strip():
                parts.append("%s=%s" % (k, redact(v)))
        return "; ".join(parts) if parts else "error object without code/type/message"
    if isinstance(err, str) and err.strip():
        return "message=" + redact(err)
    if isinstance(body, dict) and isinstance(body.get("message"), str) and body["message"].strip():
        return "message=" + redact(body["message"])
    return "no error message in response"


def short_error(raw):
    try:
        body = json.loads(raw.decode("utf-8", "replace"))
    except ValueError:
        return "non-JSON response body (%d bytes, not shown)" % len(raw) if raw else "empty response body"
    return error_summary(body)


def describe_exc(e):
    """Error type plus redacted reason; never the full str() of a request/response."""
    reason = getattr(e, "reason", None)
    if reason is None:
        reason = e
    if isinstance(reason, (socket.timeout, TimeoutError)) or isinstance(e, (socket.timeout, TimeoutError)):
        return "timeout"
    return "%s: %s" % (type(reason).__name__, redact(reason))


def config_path():
    """The image Skill config written by deeprouter: ~/.deeprouter/image-skill.json."""
    return os.path.join(os.path.expanduser("~"), CONFIG_DIR, CONFIG_FILE)


def parse_image_base(raw):
    """The API base (ending in /v1) for an image address. Raises ValueError.

    Same rules as deeprouter's imageconfig.ParseBase: https only, a host, no
    userinfo, query, or fragment; a trailing slash or /v1 is not doubled.
    """
    s = str(raw).strip()
    if not s:
        raise ValueError("the image API address is empty")
    if "?" in s or "#" in s:
        raise ValueError("the image API address must not include a query string or fragment")
    if any(c.isspace() or ord(c) < 32 or ord(c) == 127 or c == "\\" for c in s):
        raise ValueError("the image API address is not a valid URL")
    if not s.lower().startswith("https://"):
        raise ValueError("the image API address must start with https://")
    try:
        p = urllib.parse.urlsplit(s)
        port = p.port
    except ValueError:
        raise ValueError("the image API address is not a valid URL")
    if "@" in p.netloc:
        raise ValueError("the image API address must not include a username or password")
    if not p.hostname:
        raise ValueError("the image API address is missing a host")
    path = p.path.rstrip("/")
    if path.lower().endswith("/v1"):
        path = path[:-3].rstrip("/")
    host = p.hostname.lower()
    if ":" in host:
        host = "[%s]" % host
    if port is not None:
        host += ":%d" % port
    return "https://%s%s/v1" % (host, path)


def check_key(key, where):
    """A usable key: one line, not empty, not too long. Exits 2 otherwise."""
    if not isinstance(key, str) or not key.strip():
        die("%s is empty" % where, 2)
    key = key.strip()
    if any(c in key for c in "\r\n\x00") or len(key) > MAX_KEY_LEN:
        die("%s is not a single-line key of at most %d characters" % (where, MAX_KEY_LEN), 2)
    return key


def check_model(model):
    """A saved model id: same form rules as deeprouter's imageconfig.ParseModel.
    Returns the id, or None when it is not usable."""
    if not isinstance(model, str):
        return None
    m = model.strip()
    if (not m or len(m.encode("utf-8", "surrogatepass")) > MAX_MODEL_LEN
            or any(c.isspace() or ord(c) < 32 or 127 <= ord(c) < 160 or 0xD800 <= ord(c) <= 0xDFFF or c == "\\" for c in m)
            or m.startswith("/") or ".." in m or "://" in m):
        return None
    return m


def read_image_config(path):
    """(api base, key, model or None) from the image Skill config, or None when
    the file is missing or lacks an address or a key. An unsafe or broken file
    exits 2."""
    def reject(why):
        die("image Skill config %s rejected: %s. Nothing was sent. Run deeprouter again to "
            "set the image address and token, or delete the file." % (CONFIG_SHOWN, why), 2)

    try:
        st = os.lstat(path)
    except FileNotFoundError:
        return None
    except OSError as e:
        reject("cannot read it (%s)" % (e.strerror or type(e).__name__))
    if stat.S_ISLNK(st.st_mode):
        reject("it is a symbolic link")
    if not stat.S_ISREG(st.st_mode):
        reject("not a regular file")
    try:
        fd = os.open(path, os.O_RDONLY | getattr(os, "O_BINARY", 0) | getattr(os, "O_NOFOLLOW", 0))
    except OSError as e:
        reject("cannot open it (%s)" % (e.strerror or type(e).__name__))
    with os.fdopen(fd, "rb") as f:
        if not stat.S_ISREG(os.fstat(f.fileno()).st_mode):
            reject("not a regular file")
        raw = f.read(MAX_CONFIG_BYTES + 1)
    if len(raw) > MAX_CONFIG_BYTES:
        reject("larger than %d bytes" % MAX_CONFIG_BYTES)
    try:
        doc = json.loads(raw.decode("utf-8"))
    except (ValueError, UnicodeDecodeError):
        reject("not valid JSON (contents not shown)")
    if not isinstance(doc, dict):
        reject("not a JSON object")
    base, key = doc.get("base_url"), doc.get("api_key")
    if not (isinstance(base, str) and base.strip() and isinstance(key, str) and key.strip()):
        return None
    try:
        api = parse_image_base(base)
    except ValueError as e:
        reject(str(e))
    key = check_key(key, "the api_key in " + CONFIG_SHOWN)
    model = doc.get("model")
    if model is None or (isinstance(model, str) and not model.strip()):
        return api, key, None
    if check_model(model) is None:
        reject("the model is not a single model id")
    return api, key, check_model(model)


def resolve_api(host):
    """(api base, key, saved model or None) for generate/edit. The three sources
    are never mixed; only the config file can name a model."""
    env_base = os.environ.get(ENV_IMAGE_BASE, "").strip()
    env_key = os.environ.get(ENV_IMAGE_KEY, "").strip()
    if env_base or env_key:
        if not (env_base and env_key):
            die("set both %s and %s, or neither; only %s is set. Nothing was sent."
                % (ENV_IMAGE_BASE, ENV_IMAGE_KEY, ENV_IMAGE_BASE if env_base else ENV_IMAGE_KEY), 2)
        if host is not None:
            die("--host applies only to %s; %s is set. Nothing was sent." % (ENV_LEGACY_KEY, ENV_IMAGE_BASE), 2)
        try:
            api = parse_image_base(env_base)
        except ValueError as e:
            die("%s: %s. Nothing was sent." % (ENV_IMAGE_BASE, e), 2)
        return api, check_key(env_key, ENV_IMAGE_KEY), None
    found = read_image_config(config_path())
    if found:
        if host is not None:
            die("--host applies only to %s; the image Skill config %s sets the address. Nothing was sent."
                % (ENV_LEGACY_KEY, CONFIG_SHOWN), 2)
        return found
    key = os.environ.get(ENV_LEGACY_KEY, "").strip()
    if not key:
        die("no image API token. Run deeprouter and choose Image Skill to save an address and token, "
            "or set %s and %s, or set %s yourself." % (ENV_IMAGE_BASE, ENV_IMAGE_KEY, ENV_LEGACY_KEY), 2)
    return "https://%s/v1" % (host or DEFAULT_HOST), check_key(key, ENV_LEGACY_KEY), None


def api_call(url, body, content_type, timeout, key):
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Authorization", "Bearer " + key)
    req.add_header("Content-Type", content_type)
    req.add_header("Accept", "application/json")
    where = safe_url(url)
    status = None
    try:
        with OPENER.open(req, timeout=timeout) as resp:
            status = getattr(resp, "status", None)
            raw = resp.read()
    except urllib.error.HTTPError as e:
        try:
            if 300 <= e.code < 400:
                msg = "HTTP %d from %s: redirect refused (not followed, not retried)" % (e.code, where)
            else:
                try:
                    raw_err = e.read() if e.fp else b""
                except Exception:
                    raw_err = b""
                msg = ("HTTP %d from %s: %s. Status alone does not identify the cause; not retrying."
                       % (e.code, where, short_error(raw_err)))
        finally:
            close_quietly(e)
        die(msg)
    except (urllib.error.URLError, OSError) as e:
        die("request to %s failed (%s). It may still have been processed and billed; not retrying." % (where, describe_exc(e)))
    except Exception as e:
        die("request to %s failed (%s). It may still have been processed and billed; not retrying." % (where, type(e).__name__))
    try:
        return json.loads(raw.decode("utf-8"))
    except ValueError:
        die("HTTP %s from %s: response is not JSON (%d bytes, not shown)" % (status or "?", where, len(raw)))


def close_quietly(resource):
    try:
        resource.close()
    except Exception:
        pass


class DownloadError(Exception):
    """A result download failed; the message is already safe (host and status only)."""


class UnsafeURL(DownloadError):
    """The result URL itself is refused (not HTTPS, userinfo, local/private host)."""


def ip_is_public(ip):
    if ip.version == 6 and ip.ipv4_mapped:
        ip = ip.ipv4_mapped
    return ip.is_global and not ip.is_multicast and not ip.is_reserved


def check_result_url(url):
    """Static checks on a result URL: https, no userinfo, public-looking host. Returns the host."""
    if not isinstance(url, str) or not url or len(url) > MAX_URL_LEN:
        raise UnsafeURL("refusing result URL (empty or too long)")
    try:
        p = urllib.parse.urlsplit(url)
        host = (p.hostname or "").lower()
        p.port  # raises ValueError on a bad port
    except ValueError:
        raise UnsafeURL("refusing malformed result URL")
    if p.scheme != "https":
        raise UnsafeURL("refusing non-HTTPS result URL")
    if "@" in p.netloc or any(c in url for c in "\r\n\t \\"):
        raise UnsafeURL("refusing result URL with userinfo or control characters")
    if not host or "%" in host:
        raise UnsafeURL("refusing result URL without a usable host")
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        ip = None
    if ip is not None:
        if not ip_is_public(ip):
            raise UnsafeURL("refusing result URL to a non-public address (%s)" % host)
        return host
    labels = host.rstrip(".").split(".")
    if (not re.match(r"[a-z0-9.\-]+\Z", host) or len(labels) < 2 or host.rstrip(".") in BLOCKED_HOSTS
            or host.rstrip(".").endswith(BLOCKED_SUFFIXES) or labels[-1].isdigit() or labels[-1].startswith("0x")):
        raise UnsafeURL("refusing result URL to a local or non-public host (%s)" % host)
    return host


def resolve_host(host, port):
    """All addresses host resolves to. Replaced in tests."""
    return [info[4][0] for info in socket.getaddrinfo(host, port, 0, socket.SOCK_STREAM)]


def check_resolved(host, port):
    """Refuse a host name that resolves to any non-public address."""
    try:
        ipaddress.ip_address(host)
        return
    except ValueError:
        pass
    try:
        addrs = resolve_host(host, port)
    except (OSError, UnicodeError):
        raise DownloadError("download failed from %s (cannot resolve host)" % host)
    if not addrs:
        raise DownloadError("download failed from %s (cannot resolve host)" % host)
    for addr in addrs:
        try:
            ip = ipaddress.ip_address(str(addr).split("%")[0])
        except ValueError:
            raise DownloadError("download failed from %s (unusable address)" % host)
        if not ip_is_public(ip):
            raise DownloadError("refusing download: %s resolves to a non-public address (a local fake-IP DNS, "
                                "e.g. 198.18.x.x from a proxy app, can cause this; see the script docstring)" % host)


def explicit_proxy_for(url):
    """The explicit HTTP proxy OPENER will tunnel this https URL through (CONNECT), else None.

    Only https_proxy / HTTPS_PROXY in this process's environment count, and only when
    OPENER's own ProxyHandler handles https with that same value, NO_PROXY / no_proxy
    does not bypass the host, and the value is a plain http://host[:port] proxy.
    System (macOS/Windows) proxy settings, TUN, ALL_PROXY and HTTP_PROXY never count:
    urllib does not use the last two for https, and system settings are not an
    explicit choice. Anything else returns None and the direct DNS check applies.
    """
    env = urllib.request.getproxies_environment()
    proxy = env.get("https")
    if not isinstance(proxy, str) or not proxy or any(c in proxy for c in "\r\n\t \\"):
        return None
    handler = None
    for h in getattr(OPENER, "handle_open", {}).get("https", ()):
        if isinstance(h, urllib.request.ProxyHandler):
            handler = h
    if handler is None or getattr(handler, "proxies", {}).get("https") != proxy:
        return None
    try:
        p = urllib.parse.urlsplit(proxy)
        p.port  # raises ValueError on a bad port
    except ValueError:
        return None
    if p.scheme != "http" or not p.hostname or p.path not in ("", "/") or p.query or p.fragment:
        return None
    netloc = urllib.parse.urlsplit(url).netloc
    if urllib.request.proxy_bypass_environment(netloc, env) or urllib.request.proxy_bypass(netloc):
        return None
    return proxy


def download(url, timeout):
    """One plain GET (no Authorization, Cookie or Referer; redirects refused). Raises DownloadError."""
    host = check_result_url(url)
    if explicit_proxy_for(url) is None:
        check_resolved(host, urllib.parse.urlsplit(url).port or 443)
    # else: the explicit proxy resolves the host (HTTP CONNECT), so a local fake-IP DNS
    # answer is irrelevant and is not checked; this trusts that proxy's egress. The
    # static URL checks above still apply.
    req = urllib.request.Request(url, method="GET")
    try:
        with OPENER.open(req, timeout=timeout) as resp:
            data = resp.read(MAX_DOWNLOAD_BYTES + 1)
    except urllib.error.HTTPError as e:
        code = e.code
        close_quietly(e)
        raise DownloadError("download HTTP %d from %s" % (code, host))
    except (urllib.error.URLError, OSError) as e:
        raise DownloadError("download failed from %s (%s)" % (host, describe_exc(e)))
    except Exception as e:
        raise DownloadError("download failed from %s (%s)" % (host, type(e).__name__))
    if len(data) > MAX_DOWNLOAD_BYTES:
        raise DownloadError("download from %s exceeds size limit" % host)
    return data


def make_dirs(path):
    """Create missing directories with mode 0700; directories that already exist are left alone."""
    path = os.path.abspath(path)
    missing = []
    while not os.path.isdir(path):
        missing.append(path)
        parent = os.path.dirname(path)
        if parent == path:
            break
        path = parent
    for d in reversed(missing):
        os.mkdir(d, 0o700)


def write_recovery(out_dir, items, timeout):
    """Write the still-needed result URLs to a new 0600 file in out_dir; returns its path.

    Only url, file name stem, fallback extension, item index and a redacted error
    are stored: no key, headers, response body or Base64.
    """
    doc = {"kind": RECOVER_KIND, "version": RECOVER_VERSION, "note": RECOVER_NOTE,
           "created": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
    if isinstance(timeout, (int, float)) and 0 < timeout <= 86400:
        doc["timeout"] = timeout
    doc["items"] = [{k: it[k] for k in ("index", "url", "stem", "fallback_ext", "error") if k in it} for it in items]
    data = (json.dumps(doc, indent=2) + "\n").encode("utf-8")
    base = os.path.join(out_dir, RECOVER_PREFIX + time.strftime("%Y%m%d-%H%M%S"))
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_BINARY", 0) | getattr(os, "O_NOFOLLOW", 0)
    i = 1
    while True:
        path = base + ("" if i == 1 else "-%d" % i) + ".json"
        try:
            fd = os.open(path, flags, 0o600)
            break
        except FileExistsError:
            i += 1
    with os.fdopen(fd, "wb") as f:
        try:
            # umask can only narrow 0600. On Windows this only sets the read-only bit; the
            # file keeps the folder's inherited ACL and is not encrypted.
            if hasattr(os, "fchmod"):
                os.fchmod(f.fileno(), 0o600)
            else:
                os.chmod(path, 0o600)
        except OSError:
            pass
        f.write(data)
    return path


def warn(msg):
    print("error: " + redact(msg), file=sys.stderr)


def fetch_items(items, out_dir, timeout):
    """Download each {index, url, stem, fallback_ext} once. Returns (saved, failed, refused)."""
    saved, failed, refused = [], [], 0
    for it in items:
        try:
            data = download(it["url"], timeout)
        except UnsafeURL as e:
            warn("image %d: %s; not saved to a recovery file" % (it["index"], e))
            refused += 1
            continue
        except DownloadError as e:
            warn("image %d: %s" % (it["index"], e))
            failed.append(dict(it, error=redact(e, limit=120)))
            continue
        saved.append(write_new(out_dir, it["stem"], data, it["fallback_ext"]))
    return saved, failed, refused


def save_results(body, out_dir, stem, fallback_ext, timeout, what):
    items = body.get("data") if isinstance(body, dict) else None
    items = [items] if isinstance(items, dict) else (items if isinstance(items, list) else [])
    if not items:
        die("no image in response (%s)" % error_summary(body))
    fallback_ext = safe_ext(fallback_ext)
    make_dirs(out_dir)
    saved, urls, bad = [], [], 0
    for i, item in enumerate(items, 1):
        if not isinstance(item, dict):
            continue
        name = "%s-%d" % (stem, i)
        if isinstance(item.get("b64_json"), str) and item["b64_json"]:
            try:
                data = base64.b64decode("".join(item["b64_json"].split()), validate=True)
            except (binascii.Error, ValueError):
                warn("image %d: invalid Base64 (not shown)" % i)
                bad += 1
                continue
            saved.append(write_new(out_dir, name, data, fallback_ext))
        elif isinstance(item.get("url"), str) and item["url"]:
            urls.append({"index": i, "url": item["url"], "stem": name, "fallback_ext": fallback_ext})
    got, failed, refused = fetch_items(urls, out_dir, timeout)
    saved += got
    bad += refused
    if not saved and not failed and not bad:
        die("response contained no b64_json or url image fields")
    for p in saved:
        print("saved " + os.path.abspath(p))
    meta = {k: redact(body.get(k), limit=60) for k in ("created", "size", "quality", "output_format", "background")
            if isinstance(body.get(k), (str, int, float))}
    if meta:
        print("metadata " + json.dumps(meta, ensure_ascii=False))
    if any(p.endswith(".svg") for p in saved):
        print("note: SVG saved as untrusted data; do not execute embedded scripts or follow its links")
    if failed:
        rec = os.path.abspath(write_recovery(out_dir, failed, timeout))
        print("recovery file: " + rec, flush=True)
        die("the %s request succeeded but %d of %d result download(s) failed; it was not resent and the download "
            "was not retried. Complete result URLs are only in the private file on the 'recovery file:' line (do not "
            "share or commit it). Retry only the download (no new %s request): "
            "python3 scripts/deeprouter_image.py recover --from <recovery file>" % (what, len(failed), len(urls), what))
    if bad:
        die("%d image(s) in the response could not be saved" % bad)


def _reject_dup_keys(pairs):
    keys = [k for k, _ in pairs]
    if len(keys) != len(set(keys)):
        raise ValueError("duplicate key")
    return dict(pairs)


def _reject_constant(name):
    raise ValueError("non-finite number")


def _is_num(v):
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def load_recovery(path):
    """Read and strictly validate a recovery file. Exits 2 without any request if it is not acceptable."""
    def reject(why):
        die("recovery file rejected: %s. Nothing was downloaded or sent." % why, 2)

    try:
        st = os.lstat(path)
    except OSError as e:
        reject("cannot read it (%s)" % (e.strerror or type(e).__name__))
    if stat.S_ISLNK(st.st_mode):
        reject("it is a symbolic link")
    try:
        fd = os.open(path, os.O_RDONLY | getattr(os, "O_BINARY", 0) | getattr(os, "O_NOFOLLOW", 0))
    except OSError as e:
        reject("cannot open it (%s)" % (e.strerror or type(e).__name__))
    if not stat.S_ISREG(os.fstat(fd).st_mode):
        os.close(fd)
        reject("not a regular file")
    with os.fdopen(fd, "rb") as f:
        raw = f.read(MAX_RECOVER_BYTES + 1)
    if len(raw) > MAX_RECOVER_BYTES:
        reject("larger than %d bytes" % MAX_RECOVER_BYTES)
    try:
        doc = json.loads(raw.decode("utf-8"), object_pairs_hook=_reject_dup_keys, parse_constant=_reject_constant)
    except (ValueError, UnicodeDecodeError):
        reject("not valid JSON (contents not shown)")
    if not isinstance(doc, dict) or doc.get("kind") != RECOVER_KIND or doc.get("version") != RECOVER_VERSION:
        reject("unknown kind or version")
    if set(doc) - {"kind", "version", "note", "created", "timeout", "items"}:
        reject("unexpected top-level field")
    for k, limit in (("note", 500), ("created", 40)):
        if k in doc and not (isinstance(doc[k], str) and len(doc[k]) <= limit):
            reject("invalid %s" % k)
    timeout = doc.get("timeout", 600)
    if not _is_num(timeout) or not 0 < timeout <= 86400:
        reject("invalid timeout")
    items = doc.get("items")
    if not isinstance(items, list) or not 1 <= len(items) <= MAX_RECOVER_ITEMS:
        reject("items must be a list of 1 to %d objects" % MAX_RECOVER_ITEMS)
    out, stems = [], set()
    for n, it in enumerate(items, 1):
        if not isinstance(it, dict):
            reject("item %d is not an object" % n)
        if set(it) - {"index", "url", "stem", "fallback_ext", "error"}:
            reject("item %d has an unexpected field" % n)
        index = it.get("index", n)
        if isinstance(index, bool) or not isinstance(index, int) or not 1 <= index <= 1000:
            reject("item %d has an invalid index" % n)
        stem = it.get("stem", "deeprouter-recovered-%d" % n)
        if not isinstance(stem, str) or not _STEM_RE.match(stem) or stem in stems:
            reject("item %d has an invalid or duplicate file name stem" % n)
        stems.add(stem)
        ext = it.get("fallback_ext", "png")
        if not isinstance(ext, str) or ext not in SAFE_EXT:
            reject("item %d has an invalid fallback_ext" % n)
        if "error" in it and not (isinstance(it["error"], str) and len(it["error"]) <= 300):
            reject("item %d has an invalid error field" % n)
        if not isinstance(it.get("url"), str):
            reject("item %d has no url string" % n)
        try:
            check_result_url(it["url"])
        except UnsafeURL as e:
            reject("item %d: %s" % (n, e))
        out.append({"index": index, "url": it["url"], "stem": stem, "fallback_ext": SAFE_EXT[ext]})
    return out, timeout


def run_recover(a):
    """Retry only the result downloads listed in a recovery file. Never calls the image API."""
    items, timeout = load_recovery(a.from_)
    if a.timeout is not None:
        if not 0 < a.timeout <= 86400:
            die("--timeout must be between 0 and 86400 seconds", 2)
        timeout = a.timeout
    out_dir = a.out_dir if a.out_dir is not None else os.path.dirname(os.path.abspath(a.from_))
    if not os.path.isdir(out_dir):
        die("--out-dir must be an existing directory", 2)
    saved, failed, refused = fetch_items(items, out_dir, timeout)
    for p in saved:
        print("saved " + os.path.abspath(p))
    if not failed and not refused:
        print("note: the recovery file %s still contains the complete result URLs; it was not deleted. "
              "Delete it when you no longer need it." % os.path.abspath(a.from_))
        return
    if failed and saved:
        rec = os.path.abspath(write_recovery(out_dir, failed, timeout))
        print("recovery file (remaining downloads): " + rec, flush=True)
        left = "the remaining URLs are in the new recovery file printed on stdout"
    else:
        left = "the original recovery file was left unchanged"
    die("recover: %d of %d download(s) failed; %s. Nothing was sent to the image API and nothing was retried."
        % (len(failed) + refused, len(items), left))


def find_credential_keys(obj, prefix=""):
    found = []
    if isinstance(obj, dict):
        for k, v in obj.items():
            norm = str(k).strip().lower().replace("-", "_")
            if norm in CREDENTIAL_KEYS or any(part in norm for part in CREDENTIAL_PARTS):
                found.append(prefix + str(k))
            found.extend(find_credential_keys(v, prefix + str(k) + "."))
    elif isinstance(obj, list):
        for v in obj:
            found.extend(find_credential_keys(v, prefix))
    return found


def validate_generate_body(body):
    """Checked after --extra-json is merged, so extra fields cannot bypass it."""
    for k in ("model", "prompt"):
        if not isinstance(body.get(k), str) or not body[k].strip():
            die("%s must be a non-empty string" % k, 2)
    n = body.get("n")
    if isinstance(n, bool) or not isinstance(n, int) or not 1 <= n <= 10:
        die("n must be an integer between 1 and 10", 2)


def build_generate_body(a):
    """The JSON body. Without --model the model is DEFAULT_MODEL for now; run()
    puts the saved one in once the config is read."""
    body = {"model": DEFAULT_MODEL if a.model is None else a.model, "prompt": a.prompt, "n": a.n}
    for k in ("size", "quality", "format"):
        if getattr(a, k):
            body[k] = getattr(a, k)
    if a.extra_json:
        try:
            extra = json.loads(a.extra_json)
        except ValueError:
            die("--extra-json is not valid JSON", 2)
        if not isinstance(extra, dict):
            die("--extra-json must be a JSON object", 2)
        bad = find_credential_keys(extra)
        if bad:
            die("--extra-json must not contain credential-like keys (%s); the key is read only from "
                "the environment or the image Skill config. Not sent." % ", ".join(sorted(set(b[:40] for b in bad))), 2)
        body.update(extra)
        a.extra_model = "model" in extra
    validate_generate_body(body)
    return body


def fallback_ext_for(body):
    """Extension from format/output_format, restricted to SAFE_EXT; anything else -> png."""
    for k in ("format", "output_format"):
        ext = safe_ext(body.get(k), default=None)
        if ext:
            return ext
    return "png"


def multipart(fields, files):
    boundary = "----deeprouter" + uuid.uuid4().hex
    out = []
    for name, value in fields:
        out.append(("--%s\r\nContent-Disposition: form-data; name=\"%s\"\r\n\r\n%s\r\n" % (boundary, name, value)).encode("utf-8"))
    for name, path in files:
        try:
            with open(path, "rb") as f:
                data = f.read()
        except OSError as e:
            die("cannot read %s: %s" % (path, e.strerror), 2)
        ext = sniff_ext(data, "png") if data else "png"
        ctype = {"png": "image/png", "jpg": "image/jpeg", "webp": "image/webp", "gif": "image/gif"}.get(ext, "application/octet-stream")
        fname = os.path.basename(path).replace('"', "_").replace("\r", "_").replace("\n", "_")
        out.append(("--%s\r\nContent-Disposition: form-data; name=\"%s\"; filename=\"%s\"\r\nContent-Type: %s\r\n\r\n" % (boundary, name, fname, ctype)).encode("utf-8"))
        out.append(data + b"\r\n")
    out.append(("--%s--\r\n" % boundary).encode())
    return b"".join(out), "multipart/form-data; boundary=" + boundary


def build_parser():
    p = argparse.ArgumentParser(description="One DeepRouter image request (no retries). Address and key from "
                                "DEEPROUTER_IMAGE_BASE_URL + DEEPROUTER_IMAGE_API_KEY, else ~/.deeprouter/image-skill.json, "
                                "else DEEPROUTER_API_KEY with --host.")
    sub = p.add_subparsers(dest="cmd", required=True)
    for name in ("generate", "edit"):
        s = sub.add_parser(name)
        s.add_argument("--prompt", required=True)
        s.add_argument("--model", help="default: the model saved with deeprouter (Image Skill), else %s" % DEFAULT_MODEL)
        s.add_argument("--n", type=int, default=1)
        s.add_argument("--size", help="e.g. 1024x1024, 1536x1024, 1024x1536, auto")
        s.add_argument("--quality", help="low, medium, high, auto")
        s.add_argument("--host", choices=API_HOSTS,
                       help="official API host for DEEPROUTER_API_KEY (default deeprouter.top, no failover); "
                            "not allowed with the image address settings")
        s.add_argument("--out-dir", default=".")
        s.add_argument("--timeout", type=float, default=600)
        s.add_argument("--confirm", action="store_true", help="required: acknowledge the request may be billed")
        if name == "generate":
            s.add_argument("--format", choices=("png", "jpeg", "webp"), help="documented static format")
            s.add_argument("--extra-json", help="advanced, undocumented fields merged into the JSON body (no credentials)")
        else:
            s.add_argument("--image", action="append", required=True, help="input image file (repeatable)")
            s.add_argument("--mask", help="optional PNG mask, same size as the first image")
            s.add_argument("--background", help="documented: opaque or auto")
            s.add_argument("--moderation", choices=("low", "auto"))
    r = sub.add_parser("recover", help="retry only the result download from a recovery file (no API request, no key)")
    r.add_argument("--from", dest="from_", required=True, metavar="FILE", help="deeprouter-download-recover-*.json")
    r.add_argument("--out-dir", help="existing directory (default: the recovery file's directory)")
    r.add_argument("--timeout", type=float, help="seconds (default: from the recovery file, else 600)")
    return p


def use_api(host):
    base, key, model = resolve_api(host)
    _ACTIVE_KEYS.append(key)
    return base, key, model


def pick_model(flag, saved):
    """--model, else the saved model, else DEFAULT_MODEL."""
    if flag is not None:
        return flag
    return saved or DEFAULT_MODEL


def run(argv=None):
    a = build_parser().parse_args(argv)

    if a.cmd == "recover":
        return run_recover(a)

    if not 1 <= a.n <= 10:
        die("--n must be between 1 and 10", 2)

    stem = "deeprouter-" + a.cmd

    if a.cmd == "generate":
        body = build_generate_body(a)
        if not a.confirm:
            die("not sent: pass --confirm after telling the user this request may be billed", 2)
        fallback = fallback_ext_for(body)
        base, key, saved = use_api(a.host)
        if not getattr(a, "extra_model", False):
            body["model"] = pick_model(a.model, saved)
        resp = api_call(base + "/images/generations", json.dumps(body).encode("utf-8"), "application/json", a.timeout, key)
    else:
        if not a.prompt.strip() or (a.model is not None and not a.model.strip()):
            die("model and prompt must be non-empty", 2)
        if not a.confirm:
            die("not sent: pass --confirm after telling the user this request may be billed", 2)
        base, key, saved = use_api(a.host)
        fields = [("model", pick_model(a.model, saved)), ("prompt", a.prompt), ("n", str(a.n))]
        for k in ("size", "quality", "background", "moderation"):
            if getattr(a, k):
                fields.append((k, getattr(a, k)))
        files = [("image", path) for path in a.image]
        if a.mask:
            files.append(("mask", a.mask))
        data, ctype = multipart(fields, files)
        fallback = "png"
        resp = api_call(base + "/images/edits", data, ctype, a.timeout, key)

    save_results(resp, a.out_dir, stem, fallback, a.timeout, a.cmd)


def main(argv=None):
    try:
        run(argv)
    except (SystemExit, KeyboardInterrupt):
        raise
    except Exception as e:
        die("unexpected %s (details withheld to avoid leaking secrets)" % type(e).__name__)


if __name__ == "__main__":
    main()
