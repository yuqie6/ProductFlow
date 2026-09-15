#!/usr/bin/env python3
"""Drive a logged-in Chrome over CDP and extract Taobao listings for the image-eval pool.

Workflow (IMG-C-01 in docs/audits/image-quality.md):
  browser session opens the page and yields URLs -> this host downloads bytes and
  admits them (`productflow-image-evals ingest`). Risk control (login wall, captcha)
  stops the run and is reported; nothing here bypasses it.

Requires a Chrome started with:
  google-chrome --remote-debugging-port=9222 --remote-allow-origins=* \
    --user-data-dir=<profile> about:blank
and python3 with `websocket-client`.

Usage:
  collect.py search --query "陶瓷马克杯" [--limit 40]
  collect.py run    --category home --query "陶瓷马克杯" --want 22 --out <dir>
  collect.py status --out <dir>
"""
from __future__ import annotations

import argparse
import json
import os
import pathlib
import random
import sys
import time
import urllib.parse
import urllib.request

import websocket

HERE = pathlib.Path(__file__).resolve().parent
PORT = int(os.environ.get("PF_CDP_PORT", "9222"))
RISK_RE = "验证码|请完成验证|滑动验证|异常访问|punish|captcha|拖放到指定区域"
EXIT_RISK = 3  # captcha / risk control: stop and report, never retry automatically
EXIT_LOGIN = 4  # session expired: wait for a human to log in again
LOGIN_RE = "login\\.taobao\\.com|亲，请登录"


# ---------------------------------------------------------------- CDP plumbing
def _http(path: str) -> dict:
    with urllib.request.urlopen(f"http://127.0.0.1:{PORT}{path}", timeout=10) as r:
        return json.loads(r.read().decode("utf-8"))


def browser_version() -> dict:
    return _http("/json/version")


def page_target() -> dict:
    for target in _http("/json/list"):
        if target.get("type") == "page":
            return target
    raise RuntimeError("no page target; is Chrome running with --remote-debugging-port?")


class CDP:
    def __init__(self, ws_url: str):
        self.ws = websocket.create_connection(ws_url, timeout=120, max_size=256 * 1024 * 1024)
        self.seq = 0

    def call(self, method: str, params: dict | None = None, timeout: int = 120):
        self.seq += 1
        mid = self.seq
        self.ws.send(json.dumps({"id": mid, "method": method, "params": params or {}}))
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.ws.settimeout(max(1.0, deadline - time.time()))
            msg = json.loads(self.ws.recv())
            if msg.get("id") == mid:
                if "error" in msg:
                    raise RuntimeError(f"{method}: {msg['error']}")
                return msg.get("result", {})
        raise TimeoutError(method)

    def evaluate(self, expr: str, await_promise: bool = False, timeout: int = 120):
        res = self.call(
            "Runtime.evaluate",
            {"expression": expr, "awaitPromise": await_promise, "returnByValue": True,
             "userGesture": True},
            timeout=timeout,
        )
        if res.get("exceptionDetails"):
            raise RuntimeError("JS exception: " + json.dumps(res["exceptionDetails"])[:600])
        return res.get("result", {}).get("value")

    def goto(self, url: str, wait: float = 4.0, timeout: int = 120) -> str:
        self.call("Page.navigate", {"url": url}, timeout=timeout)
        deadline = time.time() + timeout
        while time.time() < deadline:
            if self.evaluate("document.readyState") == "complete":
                break
            time.sleep(0.4)
        time.sleep(wait)
        return self.evaluate("location.href")

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


def connect() -> CDP:
    cdp = CDP(page_target()["webSocketDebuggerUrl"])
    cdp.call("Page.enable")
    cdp.call("Runtime.enable")
    return cdp


# ------------------------------------------------------------------- page state
def page_state(cdp: CDP) -> dict:
    raw = cdp.evaluate(
        """(() => {
  const text = (document.body.innerText || '');
  const blob = text + ' ' + document.title;
  const m = blob.match(/""" + RISK_RE + """/i);
  return JSON.stringify({
    href: location.href,
    title: document.title,
    login: /login\\.taobao\\.com|亲，请登录/.test(location.href + text),
    risk: m ? m[0] : null,
    len: text.length,
  });
})()"""
    )
    return json.loads(raw)


def guard(cdp: CDP, where: str, url: str = "", wait_login: int = 0, wait_risk: int = 0) -> dict:
    """Stop on risk control; on an expired session or a challenge, optionally wait for a
    human to clear it in the browser window. Nothing here solves a challenge itself."""
    login_deadline = time.time() + wait_login
    risk_deadline = time.time() + wait_risk
    while True:
        state = page_state(cdp)
        if state["risk"]:
            if time.time() >= risk_deadline:
                print(f"RISK[{where}]: {state['risk']} @ {state['href'][:120]}", file=sys.stderr)
                print(f"RISK[{where}]: {state['risk']}", flush=True)
                sys.exit(EXIT_RISK)
            print(f"WAITING_RISK[{where}]: '{state['risk']}' - clear it in the Chrome window "
                  f"({int(risk_deadline - time.time())}s left)", file=sys.stderr, flush=True)
            time.sleep(15)
            if url:
                cdp.goto(url, wait=3.0)
            continue
        if not state["login"]:
            return state
        if time.time() >= login_deadline:
            print(f"LOGIN[{where}]: session expired @ {state['href'][:100]}", file=sys.stderr)
            print(f"LOGIN[{where}]: session expired", flush=True)
            sys.exit(EXIT_LOGIN)
        print(f"WAITING_LOGIN[{where}]: scan the QR in the Chrome window ({int(login_deadline - time.time())}s left)",
              file=sys.stderr, flush=True)
        time.sleep(15)
        if url:
            cdp.goto(url, wait=2.0)


def load_js(name: str) -> str:
    return (HERE / name).read_text(encoding="utf-8")


# ----------------------------------------------------------------------- search
def card_count(cdp: CDP) -> int:
    n = cdp.evaluate(
        "document.querySelectorAll(\"a[href*='item.taobao.com'],a[href*='detail.tmall.com']\").length"
    )
    return n if isinstance(n, int) else 0


def wait_out_soft_block(cdp: CDP, url: str, where: str, wait_risk: int) -> None:
    """Taobao answers a flagged session with a rendered search page whose results never
    load (\u201c\u52a0\u8f7d\u4e2d\u2026\u201d, 0 cards). Treat it as risk control: wait for it to decay."""
    deadline = time.time() + wait_risk
    while card_count(cdp) < 5:
        if time.time() >= deadline:
            print(f"BLOCK[{where}]: search results withheld (0 cards)", file=sys.stderr)
            print(f"BLOCK[{where}]: search results withheld", flush=True)
            sys.exit(EXIT_RISK)
        print(f"WAITING_BLOCK[{where}]: results withheld, retrying in 60s "
              f"({int(deadline - time.time())}s left)", file=sys.stderr, flush=True)
        time.sleep(60)
        cdp.goto(url, wait=4.0)


def search(cdp: CDP, query: str, limit: int = 40, wait_login: int = 0, wait_risk: int = 0) -> list[dict]:
    url = "https://s.taobao.com/search?q=" + urllib.parse.quote(query)
    print(f"search: {query} -> {cdp.goto(url, wait=3.0)[:100]}", file=sys.stderr)
    for _ in range(30):
        if card_count(cdp) >= 5:
            break
        time.sleep(1.0)
    guard(cdp, f"search:{query}", url, wait_login, wait_risk)
    wait_out_soft_block(cdp, url, f"search:{query}", wait_risk)
    out = cdp.evaluate(f"({load_js('extract-search.js')})()")
    data = json.loads(out)
    items = (data.get("items") or [])[:limit]
    print(f"search: {data.get('n')} cards, take {len(items)}", file=sys.stderr)
    return items


# ----------------------------------------------------------------------- detail
ALBUM_SELECTOR = (
    '#J_ImgBooth, [class*="MainPic"] img, [class*="mainPic"] img, [class*="thumbnail"] img, '
    '[class*="Thumb"] img, .tb-thumb img, ul.thumbnails img, [class*="PicGallery"] img, #J_UlThumb img'
)


def wait_for_album(cdp: CDP, timeout: float = 30.0) -> int:
    """The main-image album renders after the detail scroll settles; extracting too
    early yields 3 images instead of the full 5 and the case is rejected as thin."""
    deadline = time.time() + timeout
    best = 0
    while time.time() < deadline:
        n = cdp.evaluate(f"document.querySelectorAll(`{ALBUM_SELECTOR}`).length")
        best = max(best, n if isinstance(n, int) else 0)
        if best >= 5:
            return best
        time.sleep(1.0)
    return best


def detail(cdp: CDP, item_id: str, category: str, query: str, wait_login: int = 0, wait_risk: int = 0) -> dict:
    url = f"https://detail.tmall.com/item.htm?id={item_id}"
    final = cdp.goto(url, wait=4.0)
    if "detail.tmall.com" not in final and "item.taobao.com" not in final:
        # tmall ids sometimes only resolve on item.taobao.com
        final = cdp.goto(f"https://item.taobao.com/item.htm?id={item_id}", wait=4.0)
    guard(cdp, f"detail:{item_id}", url, wait_login, wait_risk)
    cdp.evaluate(f"({load_js('load-detail.js')})()", await_promise=True, timeout=180)
    guard(cdp, f"detail:{item_id}:after-load", url, wait_login, wait_risk)
    album = wait_for_album(cdp)
    listing = extract(cdp, category, query)
    # Only a page that had enough thumbnails is worth re-reading: a gallery below 5
    # with fewer thumbnails in the DOM means the listing is genuinely thin.
    if album >= 5 and len(listing.get("gallery") or []) < 5:
        print(f"  album={album} gallery={len(listing.get('gallery') or [])}; re-extract", file=sys.stderr)
        human_pause(4.0, 6.0)
        listing = extract(cdp, category, query)
    return listing


def extract(cdp: CDP, category: str, query: str) -> dict:
    out = cdp.evaluate(
        f"({load_js('extract-taobao.js')})({json.dumps(category)},{json.dumps(query)})",
        await_promise=True,
        timeout=180,
    )
    listing = json.loads(out)
    listing.setdefault("source", "taobao")
    return listing


def summarize(listing: dict) -> str:
    return (
        f"gallery={len(listing.get('gallery') or [])} "
        f"sku={len(listing.get('sku_images') or [])} "
        f"detail={len(listing.get('detail_images') or [])} "
        f"props={len(listing.get('props') or {})}"
    )


# ------------------------------------------------------------------------ files
def listing_path(out: pathlib.Path, item_id: str) -> pathlib.Path:
    return out / f"{item_id}.json"


def done_ids(out: pathlib.Path) -> set[str]:
    return {p.stem for p in out.glob("*.json")}


def status(out: pathlib.Path) -> dict:
    counts: dict[str, int] = {}
    for path in out.glob("*.json"):
        try:
            listing = json.loads(path.read_text(encoding="utf-8"))
        except Exception:
            continue
        counts[listing.get("category") or "?"] = counts.get(listing.get("category") or "?", 0) + 1
    return counts


# ------------------------------------------------------------------------- main
def cmd_search(args) -> int:
    cdp = connect()
    try:
        items = search(cdp, args.query, args.limit)
    finally:
        cdp.close()
    print(json.dumps(items, ensure_ascii=False, indent=2))
    return 0


def human_pause(lo: float, hi: float) -> None:
    """Pause like a person reading a page, not a crawler firing requests."""
    time.sleep(random.uniform(lo, hi))


def cmd_run(args) -> int:
    out = pathlib.Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    have = done_ids(out)
    cdp = connect()
    saved = kept = 0
    pages = 0
    try:
        items = search(cdp, args.query, args.limit, args.wait_login, args.wait_risk)
        for item in items:
            if saved >= args.want or pages >= args.session_max:
                break
            if item["id"] in have:
                continue
            listing = detail(cdp, item["id"], args.category, args.query,
                             args.wait_login, args.wait_risk)
            pages += 1
            title = (listing.get("title") or "").strip()
            if not title or len(title) < 4:
                print(f"skip {item['id']}: empty title", file=sys.stderr)
                continue
            listing["category"] = args.category
            listing["query"] = args.query
            listing["is_tmall"] = bool(item.get("tmall"))
            listing.setdefault("url", f"https://detail.tmall.com/item.htm?id={item['id']}")
            listing["url"] = f"https://detail.tmall.com/item.htm?id={item['id']}"
            path = listing_path(out, item["id"])
            path.write_text(json.dumps(listing, ensure_ascii=False, indent=2), encoding="utf-8")
            saved += 1
            kept += 1
            print(f"[{args.category}] {saved}/{args.want} {item['id']} {title[:38]} {summarize(listing)}",
                  flush=True)
            if pages % args.cool_after == 0:
                rest = random.uniform(args.cool_seconds, args.cool_seconds * 1.6)
                print(f"cool-down after {pages} pages: {rest:.0f}s", file=sys.stderr, flush=True)
                time.sleep(rest)
            else:
                human_pause(args.min_delay, args.max_delay)
    finally:
        cdp.close()
    if kept == 0:
        print(f"[{args.category}] no new listings for query {args.query}", file=sys.stderr)
    return 0


def cmd_status(args) -> int:
    counts = status(pathlib.Path(args.out))
    total = sum(counts.values())
    for key in sorted(counts):
        print(f"{key:12s} {counts[key]}")
    print(f"{'TOTAL':12s} {total}")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = ap.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("search")
    s.add_argument("--query", required=True)
    s.add_argument("--limit", type=int, default=40)
    s.set_defaults(func=cmd_search)

    r = sub.add_parser("run")
    r.add_argument("--category", required=True)
    r.add_argument("--query", required=True)
    r.add_argument("--want", type=int, default=22)
    r.add_argument("--out", required=True)
    r.add_argument("--limit", type=int, default=40)
    r.add_argument("--min-delay", type=float, default=8.0, help="minimum seconds between detail pages")
    r.add_argument("--max-delay", type=float, default=19.0, help="maximum seconds between detail pages")
    r.add_argument("--cool-after", type=int, default=12, help="pages before a longer rest")
    r.add_argument("--cool-seconds", type=float, default=45.0, help="base seconds for the longer rest")
    r.add_argument("--session-max", type=int, default=40, help="max detail pages in one run")
    r.add_argument("--wait-login", type=int, default=0,
                   help="seconds to wait for a human re-login before giving up (0 = fail fast)")
    r.add_argument("--wait-risk", type=int, default=0,
                   help="seconds to wait for a human to clear a captcha challenge in the window")
    r.set_defaults(func=cmd_run)

    t = sub.add_parser("status")
    t.add_argument("--out", required=True)
    t.set_defaults(func=cmd_status)

    args = ap.parse_args()
    try:
        return args.func(args)
    except SystemExit:
        raise
    except Exception as exc:  # keep batch loops informative
        print(f"ERROR: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
