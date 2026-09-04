/**
 * 详情页先点开图文详情并下滑，让懒加载模块图出现。出现滑块/验证码只回报，不破解。
 * Runtime.evaluate 传入本函数体，awaitPromise: true。
 */
(async function loadTaobaoDetail() {
  const blob = (document.body.innerText || "").slice(0, 4000);
  const risk = blob.match(/验证码|请完成验证|滑动验证|异常访问|punish|captcha/i);
  if (risk) {
    return { risk: true, signal: risk[0], href: location.href };
  }
  const tabLabels = ["图文详情", "商品详情", "详情"];
  const tab = tabLabels
    .map((label) =>
      Array.from(document.querySelectorAll("a, li, span, button")).find((el) => {
        const t = (el.textContent || "").replace(/\s+/g, "").trim();
        return t === label;
      }),
    )
    .find(Boolean);
  if (tab) {
    try {
      tab.click();
    } catch {
      /* ignore */
    }
  }
  const root =
    document.querySelector("#J_DivItemDesc, [class*='descV8'], [class*='DescV8'], [id*='desc']") ||
    document.body;
  try {
    root.scrollIntoView({ block: "start" });
  } catch {
    /* ignore */
  }
  for (let i = 0; i < 18; i++) {
    window.scrollBy(0, 900);
    await new Promise((r) => setTimeout(r, 280));
  }
  window.scrollTo(0, 0);
  await new Promise((r) => setTimeout(r, 350));
  const detailImgs = document.querySelectorAll(
    "#J_DivItemDesc img, [class*='descV8'] img, [class*='DescV8'] img",
  ).length;
  return {
    risk: false,
    href: location.href,
    title: document.title.slice(0, 80),
    detailImgs,
  };
})
