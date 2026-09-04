/**
 * 在已登录的淘宝详情页执行，抽出标题、规格、主图相册、SKU 图和详情模块图。
 * 浏览器 Runtime.evaluate 传入本函数体再调用，例如：
 *   ${file}\n("home","陶瓷马克杯")
 */
(function extractTaobaoListing(category, query) {
  function abs(url) {
    if (!url) return "";
    url = String(url).trim();
    if (url.startsWith("//")) return "https:" + url;
    return url;
  }
  function upgrade(url) {
    url = abs(url);
    url = url.split("?")[0] || url;
    url = url.replace(/_.webp$/i, "").replace(/\.avif$/i, "");
    const m = url.match(/^(https?:\/\/[^\s]+?\.(?:jpe?g|png|gif|webp|bmp))/i);
    return m ? m[1] : url;
  }
  function looksJunk(url, alt, label) {
    const blob = `${url} ${alt || ""} ${label || ""}`.toLowerCase();
    return /买家秀|用户评价|晒图|追评|评价图|buyer.?show|avatar|icon|spacer|1x1|lazyload\.gif|\/s\.gif|tps-\d+-\d+/.test(blob);
  }
  function pickUrl(img) {
    const candidates = [
      img.currentSrc,
      img.getAttribute("data-src"),
      img.getAttribute("data-ks-lazyload"),
      img.getAttribute("data-lazy-src"),
      img.src,
    ];
    for (const raw of candidates) {
      const url = abs(raw || "");
      if (url && !/\/s\.gif(\?|$)/i.test(url)) return url;
    }
    return "";
  }
  function uniq(items) {
    const seen = new Set();
    const out = [];
    for (const item of items) {
      const url = upgrade(item.url);
      if (!url || seen.has(url)) continue;
      if (!/alicdn|taobaocdn|tbcdn|tmall/.test(url)) continue;
      if (looksJunk(url, item.alt, item.label)) continue;
      seen.add(url);
      out.push({ ...item, url });
    }
    return out;
  }
  function suggestGallery(index) {
    if (index === 0) return "hero";
    if (index === 1) return "scene";
    return "detail";
  }
  const title =
    document.querySelector('[class*="ItemHeader"] h1, .tb-main-title, h1')?.textContent?.trim() ||
    document.title.replace(/-淘宝网.*$/, "").replace(/-tmall.com.*$/i, "").trim();
  const shop =
    document.querySelector('[class*="ShopHeader"] a, .tb-shop-name a, .shop-name')?.textContent?.trim() || "";
  const isTmall = /tmall\.com|天猫/.test(location.href + document.body.innerText.slice(0, 2000));
  const sales =
    Array.from(document.querySelectorAll("span, div"))
      .map((el) => el.textContent?.trim() || "")
      .find((t) => /人付款|人购买|已售|销量/.test(t) && t.length < 24) || "";
  const props = {};
  document.querySelectorAll("li, tr, [class*='Params'] span, [class*='params'] span").forEach((el) => {
    const text = el.textContent?.replace(/\s+/g, " ").trim() || "";
    const m = text.match(/^(.{1,12})[:：]\s*(.{1,80})$/);
    if (m) props[m[1]] = m[2];
  });
  const gallery = uniq(
    Array.from(
      document.querySelectorAll(
        '[class*="thumbnail"] img, [class*="Thumb"] img, .tb-thumb img, ul.thumbnails img, [class*="PicGallery"] img, #J_UlThumb img',
      ),
    ).map((img, index) => ({
      url: pickUrl(img),
      alt: img.alt || "",
      role: "gallery",
      index,
      suggested_type: suggestGallery(index),
    })),
  );
  gallery.forEach((item, index) => {
    item.index = index;
    item.suggested_type = suggestGallery(index);
  });
  const skuImages = uniq(
    Array.from(document.querySelectorAll('[class*="sku"] img, .tb-sku img, [class*="Sku"] img')).map(
      (img, index) => ({
        url: pickUrl(img),
        alt: img.alt || "",
        label: img.closest("li, a, div")?.textContent?.trim()?.slice(0, 20) || "",
        role: "sku",
        index,
        suggested_type: "sku",
      }),
    ),
  );
  const detailRoot =
    document.querySelector("#J_DivItemDesc, [class*='descV8'], [class*='DescV8'], [id*='desc'], [class*='desc-root']") ||
    document.body;
  const detailImages = uniq(
    Array.from(detailRoot.querySelectorAll("img")).map((img, index) => ({
      url: pickUrl(img),
      alt: img.alt || "",
      role: "detail",
      index,
      suggested_type: /规格|参数|尺寸|成分/.test(`${img.alt || ""}`) ? "specifications" : "selling_point",
    })),
  );
  const payload = {
    source: "taobao",
    url: location.href,
    title,
    shop,
    category: category || "",
    query: query || "",
    is_tmall: isTmall,
    sales_hint: sales,
    props,
    gallery,
    sku_images: skuImages,
    detail_images: detailImages,
  };
  window.__pfExtract = payload;
  return JSON.stringify(payload);
})
