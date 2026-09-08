import { installLocaleDictionary } from "../lib/i18n";
import zhCN from "../lib/locales/zh-CN.json";
import enUS from "../lib/locales/en-US.json";
import jaJP from "../lib/locales/ja-JP.json";
import viVN from "../lib/locales/vi-VN.json";
installLocaleDictionary("zh-CN", zhCN);
installLocaleDictionary("en-US", enUS);
installLocaleDictionary("ja-JP", jaJP);
installLocaleDictionary("vi-VN", viVN);
