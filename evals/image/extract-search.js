/**
 * 在淘宝搜索结果页执行，抽出天猫/旗舰卡片链接。不用猜你喜欢。
 * Runtime.evaluate 传入本函数体再调用。
 */
(function extractMallItems() {
  function abs(url) {
    if (!url) return "";
    url = String(url).trim();
    if (url.startsWith("//")) return "https:" + url;
    try {
      return new URL(url, location.href).href;
    } catch {
      return url;
    }
  }
  const seen = new Set();
  const items = [];
  for (const a of document.querySelectorAll("a[href]")) {
    const href = abs(a.getAttribute("href") || "");
    if (!/item\.htm|detail\.tmall/.test(href)) continue;
    let id = "";
    try {
      id = new URL(href).searchParams.get("id") || "";
    } catch {
      continue;
    }
    if (!id || seen.has(id)) continue;
    const card = a.closest("[class*='Card'], [class*='card'], [class*='item'], li, div") || a;
    const text = (card.textContent || "").replace(/\s+/g, " ").trim().slice(0, 180);
    const tmall = /tmall\.com|天猫|旗舰店/.test(href + text);
    seen.add(id);
    items.push({
      id,
      url: href.split("&spm=")[0],
      tmall,
      text,
    });
    if (items.length >= 40) break;
  }
  items.sort((a, b) => Number(b.tmall) - Number(a.tmall));
  window.__pfSearch = items;
  return JSON.stringify({ n: items.length, items: items.slice(0, 16) });
})
