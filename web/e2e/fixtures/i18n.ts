import { readFileSync } from "node:fs";
import { LOCALES, installLocaleDictionary } from "../../src/lib/i18n";
for (const locale of LOCALES) {
  installLocaleDictionary(locale, JSON.parse(readFileSync(new URL(`../../src/lib/locales/${locale}.json`, import.meta.url), "utf8")));
}
export * from "../../src/lib/i18n";
