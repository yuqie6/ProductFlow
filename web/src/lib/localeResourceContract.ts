import type zhCN from "./locales/zh-CN.json";
import type enUS from "./locales/en-US.json";
import type jaJP from "./locales/ja-JP.json";
import type viVN from "./locales/vi-VN.json";
import type { TranslationDictionary, TranslationKey } from "./i18n";

type ExactDictionary<T> = T extends TranslationDictionary ? Exclude<keyof T, TranslationKey> extends never ? true : false : false;
type Assert<T extends true> = T;
/** Erased at build time; missing or extra resource keys are compile errors. */
export type LocaleResourceContract = [Assert<ExactDictionary<typeof zhCN>>, Assert<ExactDictionary<typeof enUS>>, Assert<ExactDictionary<typeof jaJP>>, Assert<ExactDictionary<typeof viVN>>];
