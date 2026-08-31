import type { ReactNode } from "react";

import { useI18n } from "../lib/preferences";
import { TabList, TabPanel, Tabs, TabTrigger } from "./ui/tabs";

export type ImageGenerationSettingsTab = "basic" | "advanced";

interface ImageGenerationSettingsTabsProps {
  value: ImageGenerationSettingsTab;
  onChange: (value: ImageGenerationSettingsTab) => void;
  basic: ReactNode;
  advanced: ReactNode;
  className?: string;
}

export function ImageGenerationSettingsTabs({
  value,
  onChange,
  basic,
  advanced,
  className = "",
}: ImageGenerationSettingsTabsProps) {
  const { t } = useI18n();
  const tabs: readonly [ImageGenerationSettingsTab, string][] = [
    ["basic", t("imageSettings.tabs.basic")],
    ["advanced", t("imageSettings.tabs.advanced")],
  ];

  return (
    <Tabs
      value={value}
      onValueChange={(next) => onChange(next === "advanced" ? "advanced" : "basic")}
      className={className}
    >
      <TabList className="mb-4 grid min-h-11 w-full grid-cols-2 lg:min-h-9" aria-label={t("imageSettings.title")}>
        {tabs.map(([tab, label]) => (
          <TabTrigger
            key={tab}
            value={tab}
            className="min-h-10 text-sm lg:min-h-8"
          >
            {label}
          </TabTrigger>
        ))}
      </TabList>
      <TabPanel value="basic">{basic}</TabPanel>
      <TabPanel value="advanced">{advanced}</TabPanel>
    </Tabs>
  );
}
